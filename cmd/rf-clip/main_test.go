package main

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCryptoRoundTrip(t *testing.T) {
	secret := bytes.Repeat([]byte{0x42}, secretLen)
	_, key, err := deriveKeys(secret)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("hello world")
	payload, err := encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, plaintext) {
		t.Fatal("payload contains plaintext")
	}
	got, err := decrypt(key, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round trip: got %q want %q", got, plaintext)
	}

	payload[len(payload)-1] ^= 0xff
	if _, err := decrypt(key, payload); err == nil {
		t.Fatal("tampered payload decrypted without error")
	}
}

func TestDeriveKeys(t *testing.T) {
	secret := bytes.Repeat([]byte{0x01}, secretLen)
	id1, key1, err := deriveKeys(secret)
	if err != nil {
		t.Fatal(err)
	}
	id2, key2, _ := deriveKeys(secret)
	if id1 != id2 || !bytes.Equal(key1, key2) {
		t.Fatal("derivation is not deterministic")
	}
	if len(id1) != 64 || len(key1) != 32 {
		t.Fatalf("unexpected lengths: id=%d key=%d", len(id1), len(key1))
	}
	if id1 == hex.EncodeToString(key1) {
		t.Fatal("account ID equals encryption key — domain separation broken")
	}
	otherID, _, _ := deriveKeys(bytes.Repeat([]byte{0x02}, secretLen))
	if id1 == otherID {
		t.Fatal("different secrets produced the same account ID")
	}
}

func TestParseConfig(t *testing.T) {
	secret := strings.Repeat("ab", secretLen)
	c, err := parseConfig([]byte("# comment\nserver=https://clip.example.com\nsecret=" + secret + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.server != "https://clip.example.com" {
		t.Fatalf("server = %q", c.server)
	}
	if hex.EncodeToString(c.secret) != secret {
		t.Fatalf("secret mismatch")
	}

	for _, bad := range []string{"", "server=x\nsecret=nothex", "secret=" + secret} {
		if _, err := parseConfig([]byte(bad)); err == nil {
			t.Fatalf("parseConfig(%q) accepted bad config", bad)
		}
	}
}

func TestParseConfigSystemClipboard(t *testing.T) {
	base := "server=https://clip.example.com\nsecret=" + strings.Repeat("ab", secretLen) + "\n"
	for val, want := range map[string]bool{"": false, "system_clipboard=true\n": true, "system_clipboard=false\n": false} {
		c, err := parseConfig([]byte(base + val))
		if err != nil {
			t.Fatalf("parseConfig with %q: %v", val, err)
		}
		if c.systemClipboard != want {
			t.Fatalf("systemClipboard with %q = %v, want %v", val, c.systemClipboard, want)
		}
	}
	if _, err := parseConfig([]byte(base + "system_clipboard=maybe\n")); err == nil {
		t.Fatal("accepted junk system_clipboard value")
	}
}

func TestClipboardTool(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pbcopy"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("WAYLAND_DISPLAY", "")
	if tool := clipboardTool(); len(tool) == 0 || tool[0] != "pbcopy" {
		t.Fatalf("clipboardTool() = %v, want pbcopy", tool)
	}
	t.Setenv("PATH", t.TempDir())
	if tool := clipboardTool(); tool != nil {
		t.Fatalf("clipboardTool() with empty PATH = %v, want nil", tool)
	}
}
