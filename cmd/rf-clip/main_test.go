package main

import (
	"bytes"
	"encoding/hex"
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
