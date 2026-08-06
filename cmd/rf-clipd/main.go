package main

import (
	"context"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RunFridge/rf-clipboard/web"
)

type entry struct {
	Data     []byte
	LastUsed time.Time
}

var errFull = errors.New("store is full")

type store struct {
	mu         sync.Mutex
	entries    map[string]entry
	ttl        time.Duration
	maxEntries int
}

func newStore(ttl time.Duration, maxEntries int) *store {
	return &store{entries: make(map[string]entry), ttl: ttl, maxEntries: maxEntries}
}

func (s *store) put(id string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok && len(s.entries) >= s.maxEntries {
		return errFull
	}
	s.entries[id] = entry{Data: data, LastUsed: time.Now()}
	return nil
}

func (s *store) get(id string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return nil, false
	}
	e.LastUsed = time.Now()
	s.entries[id] = e
	return e.Data, true
}

func (s *store) sweep() (evicted, remaining int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.entries {
		if time.Since(e.LastUsed) > s.ttl {
			delete(s.entries, id)
			evicted++
		}
	}
	return evicted, len(s.entries)
}

// snapshot writes the store to path atomically (temp file + rename).
// Contents are client-side ciphertext, so the file is as private as the RAM was.
func (s *store) snapshot(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp, err := os.CreateTemp(filepath.Dir(path), "rf-clipd-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := gob.NewEncoder(tmp).Encode(s.entries); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (s *store) load(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	return gob.NewDecoder(f).Decode(&s.entries)
}

// accountID extracts the bearer token: 64 hex chars (SHA-256-sized), anything
// else is rejected. No registration — any well-formed ID is an account.
func accountID(r *http.Request) (string, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(tok) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(tok); err != nil {
		return "", false
	}
	return tok, true
}

func newHandler(s *store, maxSize int64) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/clip", func(w http.ResponseWriter, r *http.Request) {
		id, ok := accountID(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSize))
		if err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				http.Error(w, "clipboard too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "bad request", http.StatusBadRequest)
			}
			return
		}
		if err := s.put(id, body); err != nil {
			// capacity signal worth surfacing; never log the account ID itself
			log.Printf("put rejected: store full (%d entries)", s.maxEntries)
			http.Error(w, "server full", http.StatusInsufficientStorage)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	page := func(body []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(body)
		}
	}
	mux.HandleFunc("GET /{$}", page(web.Index))
	mux.HandleFunc("GET /privacy", page(web.Privacy))
	mux.HandleFunc("GET /ko", page(web.IndexKo))
	mux.HandleFunc("GET /ko/{$}", page(web.IndexKo))
	mux.HandleFunc("GET /ko/privacy", page(web.PrivacyKo))
	mux.HandleFunc("GET /v1/clip", func(w http.ResponseWriter, r *http.Request) {
		id, ok := accountID(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		data, ok := s.get(id)
		if !ok {
			http.Error(w, "empty", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	})
	return mux
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	addr := flag.String("addr", envOr("RF_CLIPD_ADDR", ":8080"), "listen address")
	ttlStr := flag.String("ttl", envOr("RF_CLIPD_TTL", "24h"), "evict entries unused for this long")
	maxSizeStr := flag.String("max-size", envOr("RF_CLIPD_MAX_SIZE", "1048576"), "max clipboard size in bytes")
	maxEntriesStr := flag.String("max-entries", envOr("RF_CLIPD_MAX_ENTRIES", "1000"), "max stored entries")
	persist := flag.String("persist", envOr("RF_CLIPD_PERSIST", ""), "snapshot file path (empty = memory only)")
	flag.Parse()

	ttl, err := time.ParseDuration(*ttlStr)
	if err != nil {
		log.Fatalf("invalid -ttl: %v", err)
	}
	maxSize, err := strconv.ParseInt(*maxSizeStr, 10, 64)
	if err != nil {
		log.Fatalf("invalid -max-size: %v", err)
	}
	maxEntries, err := strconv.Atoi(*maxEntriesStr)
	if err != nil {
		log.Fatalf("invalid -max-entries: %v", err)
	}

	s := newStore(ttl, maxEntries)
	if *persist != "" {
		if err := s.load(*persist); err != nil {
			log.Fatalf("load snapshot: %v", err)
		}
		if evicted, remaining := s.sweep(); evicted > 0 { // drop entries that expired while we were down
			log.Printf("sweep: evicted %d expired entries, holding %d", evicted, remaining)
		}
	}

	go func() {
		for range time.Tick(ttl / 10) {
			if evicted, remaining := s.sweep(); evicted > 0 {
				log.Printf("sweep: evicted %d expired entries, holding %d", evicted, remaining)
			}
			if *persist != "" {
				if err := s.snapshot(*persist); err != nil {
					log.Printf("snapshot: %v", err)
				}
			}
		}
	}()

	// slowloris protection when running without a reverse proxy in front
	const (
		readHeaderTimeout = 10 * time.Second
		readWriteTimeout  = 30 * time.Second
		idleTimeout       = 2 * time.Minute
	)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           newHandler(s, maxSize),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readWriteTimeout,
		WriteTimeout:      readWriteTimeout,
		IdleTimeout:       idleTimeout,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	log.Printf("rf-clipd listening on %s (ttl=%s max-size=%d max-entries=%d persist=%q)",
		*addr, ttl, maxSize, maxEntries, *persist)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	if *persist != "" {
		if err := s.snapshot(*persist); err != nil {
			log.Fatalf("final snapshot: %v", err)
		}
	}
}
