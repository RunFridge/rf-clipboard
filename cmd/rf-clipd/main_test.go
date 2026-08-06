package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func doReq(t *testing.T, h http.Handler, method, auth string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/clip", bytes.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestPutGetRoundTrip(t *testing.T) {
	h := newHandler(newStore(time.Hour, 10), 1024)
	data := []byte("ciphertext blob")

	if w := doReq(t, h, http.MethodPut, testID, data); w.Code != http.StatusNoContent {
		t.Fatalf("PUT: got %d", w.Code)
	}
	w := doReq(t, h, http.MethodGet, testID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: got %d", w.Code)
	}
	if got, _ := io.ReadAll(w.Body); !bytes.Equal(got, data) {
		t.Fatalf("GET body = %q, want %q", got, data)
	}
}

func TestGetUnknownID(t *testing.T) {
	h := newHandler(newStore(time.Hour, 10), 1024)
	other := strings.Repeat("b", 64)
	if w := doReq(t, h, http.MethodGet, other, nil); w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}
}

func TestAuthRejected(t *testing.T) {
	h := newHandler(newStore(time.Hour, 10), 1024)
	for _, auth := range []string{"", "short", strings.Repeat("z", 64)} {
		if w := doReq(t, h, http.MethodGet, auth, nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("auth %q: got %d, want 401", auth, w.Code)
		}
	}
}

func TestOversizedBody(t *testing.T) {
	h := newHandler(newStore(time.Hour, 10), 8)
	w := doReq(t, h, http.MethodPut, testID, bytes.Repeat([]byte("x"), 9))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", w.Code)
	}
}

func TestMaxEntries(t *testing.T) {
	s := newStore(time.Hour, 1)
	h := newHandler(s, 1024)
	doReq(t, h, http.MethodPut, testID, []byte("a"))
	other := strings.Repeat("b", 64)
	if w := doReq(t, h, http.MethodPut, other, []byte("b")); w.Code != http.StatusInsufficientStorage {
		t.Fatalf("got %d, want 507", w.Code)
	}
	// overwriting an existing entry is always allowed
	if w := doReq(t, h, http.MethodPut, testID, []byte("c")); w.Code != http.StatusNoContent {
		t.Fatalf("overwrite: got %d, want 204", w.Code)
	}
}

func TestSweep(t *testing.T) {
	s := newStore(time.Millisecond, 10)
	s.put(testID, []byte("x"))
	time.Sleep(5 * time.Millisecond)
	if evicted, remaining := s.sweep(); evicted != 1 || remaining != 0 {
		t.Fatalf("sweep = (%d, %d), want (1, 0)", evicted, remaining)
	}
	if _, ok := s.get(testID); ok {
		t.Fatal("expired entry survived sweep")
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snap.gob")
	s1 := newStore(time.Hour, 10)
	s1.put(testID, []byte("persisted"))
	if err := s1.snapshot(path); err != nil {
		t.Fatal(err)
	}
	s2 := newStore(time.Hour, 10)
	if err := s2.load(path); err != nil {
		t.Fatal(err)
	}
	if got, ok := s2.get(testID); !ok || !bytes.Equal(got, []byte("persisted")) {
		t.Fatalf("loaded store missing entry: %q %v", got, ok)
	}
	// loading a nonexistent file is not an error
	if err := s2.load(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatal(err)
	}
}

func TestPages(t *testing.T) {
	h := newHandler(newStore(time.Hour, 10), 1024)
	for path, want := range map[string]string{
		"/":           "rf-clipboard",
		"/privacy":    "free service",
		"/ko":         "하나의 클립보드",
		"/ko/privacy": "무료 서비스",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: got %d", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("GET %s: Content-Type = %q", path, ct)
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(want)) {
			t.Fatalf("GET %s: page missing %q", path, want)
		}
	}
}
