package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	configName     = "rf-clipboard.conf"
	secretLen      = 32
	requestTimeout = 30 * time.Second
	// ceiling on how much we'll read from a server response, in case the
	// server is compromised or misconfigured
	maxPasteBytes  = 64 << 20
	infoAccountID  = "rf-clipboard/account-id"
	infoEncryptKey = "rf-clipboard/encryption-key"
	defaultServer  = "https://clip.runfridge.dev"
	serverEnv      = "SERVER_URL"
)

type config struct {
	server          string
	secret          []byte
	systemClipboard bool
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir() // $XDG_CONFIG_HOME or ~/.config
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configName), nil
}

func parseConfig(data []byte) (config, error) {
	var c config
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return c, fmt.Errorf("malformed config line: %q", line)
		}
		switch key {
		case "server":
			c.server = val
		case "secret":
			sec, err := hex.DecodeString(val)
			if err != nil || len(sec) != secretLen {
				return c, errors.New("config: secret must be 64 hex chars")
			}
			c.secret = sec
		case "system_clipboard":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return c, fmt.Errorf("config: system_clipboard must be true or false, got %q", val)
			}
			c.systemClipboard = b
		}
	}
	if c.server == "" || c.secret == nil {
		return c, errors.New("config: missing server or secret")
	}
	return c, nil
}

func loadConfig() (config, error) {
	path, err := configPath()
	if err != nil {
		return config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config{}, fmt.Errorf("no config at %s — run 'rf-clip init <server-url>' first", path)
	}
	if err != nil {
		return config{}, err
	}
	return parseConfig(data)
}

// deriveKeys splits the secret into what the server may see (account ID) and
// what it must never see (encryption key). Server compromise reveals only
// ciphertext and a hash.
func deriveKeys(secret []byte) (accountID string, key []byte, err error) {
	id, err := hkdf.Key(sha256.New, secret, nil, infoAccountID, 32)
	if err != nil {
		return "", nil, err
	}
	key, err = hkdf.Key(sha256.New, secret, nil, infoEncryptKey, 32)
	if err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(id), key, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// encrypt returns nonce || ciphertext.
func encrypt(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(key, payload []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(payload) < gcm.NonceSize() {
		return nil, errors.New("payload too short")
	}
	nonce, ciphertext := payload[:gcm.NonceSize()], payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed: wrong secret or corrupted data")
	}
	return plaintext, nil
}

func initCmd(args []string) error {
	force := false
	if len(args) > 0 && args[0] == "-f" {
		force, args = true, args[1:]
	}
	if len(args) > 0 {
		return fmt.Errorf("init takes no arguments; set the server with %s=https://example.com rf-clip init", serverEnv)
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; re-running init would discard your secret and orphan the server-side clipboard. Use 'rf-clip init -f' to overwrite", path)
	}

	server := os.Getenv(serverEnv)
	if server == "" {
		server = defaultServer
	}
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		return fmt.Errorf("server URL must start with http:// or https://, got %q", server)
	}

	secret := make([]byte, secretLen)
	rand.Read(secret)
	content := fmt.Sprintf("server=%s\nsecret=%s\nsystem_clipboard=false\n", strings.TrimRight(server, "/"), hex.EncodeToString(secret))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s — copy this file to your other devices to share the clipboard\n", path)
	return nil
}

// clipboardTool picks the local clipboard writer for this environment;
// nil when none is available.
func clipboardTool() []string {
	candidates := [][]string{
		{"termux-clipboard-set"},             // Termux (Android)
		{"pbcopy"},                           // macOS
		{"wl-copy"},                          // Wayland
		{"xclip", "-selection", "clipboard"}, // X11
		{"xsel", "-ib"},                      // X11
		{"clip.exe"},                         // WSL
	}
	for _, c := range candidates {
		if c[0] == "wl-copy" && os.Getenv("WAYLAND_DISPLAY") == "" {
			continue
		}
		if _, err := exec.LookPath(c[0]); err == nil {
			return c
		}
	}
	return nil
}

func copyToSystem(data []byte) error {
	tool := clipboardTool()
	if tool == nil {
		return errors.New("system_clipboard is on but no clipboard tool found (termux-clipboard-set, pbcopy, wl-copy, xclip, xsel, clip.exe)")
	}
	cmd := exec.Command(tool[0], tool[1:]...)
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", tool[0], err)
	}
	return nil
}

func copyCmd() error {
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return errors.New("nothing piped to stdin; usage: <cmd> | rf-clip")
	}
	c, err := loadConfig()
	if err != nil {
		return err
	}
	accountID, key, err := deriveKeys(c.secret)
	if err != nil {
		return err
	}
	plaintext, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	// best-effort: the encrypted upload is the primary job and owns the exit code
	if c.systemClipboard {
		if err := copyToSystem(plaintext); err != nil {
			fmt.Fprintln(os.Stderr, "rf-clip: warning:", err)
		}
	}
	payload, err := encrypt(key, plaintext)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, c.server+"/v1/clip", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accountID)
	resp, err := (&http.Client{Timeout: requestTimeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return nil
}

func pasteCmd() error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	accountID, key, err := deriveKeys(c.secret)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, c.server+"/v1/clip", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accountID)
	resp, err := (&http.Client{Timeout: requestTimeout}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errors.New("clipboard is empty")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxPasteBytes))
	if err != nil {
		return err
	}
	plaintext, err := decrypt(key, payload)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(plaintext)
	return err
}

func main() {
	var err error
	switch {
	case filepath.Base(os.Args[0]) == "rf-paste":
		err = pasteCmd()
	case len(os.Args) > 1 && os.Args[1] == "init":
		err = initCmd(os.Args[2:])
	case len(os.Args) > 1 && os.Args[1] == "paste": // for go-install users without the symlink
		err = pasteCmd()
	case len(os.Args) > 1:
		err = fmt.Errorf("unknown argument %q; usage: <cmd> | rf-clip, rf-paste, rf-clip init <url>", os.Args[1])
	default:
		err = copyCmd()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rf-clip:", err)
		os.Exit(1)
	}
}
