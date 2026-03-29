package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestOriginMuxServesHLSPlaylistWithExpectedContentType(t *testing.T) {
	assetDir := t.TempDir()
	playlist := []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:2.0,\nsegment0.ts\n")

	if err := os.WriteFile(filepath.Join(assetDir, "stream.m3u8"), playlist, 0o644); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	mux := newOriginMux(assetDir)
	req := httptest.NewRequest(http.MethodGet, "/stream.m3u8", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/vnd.apple.mpegurl" {
		t.Fatalf("unexpected content type %q", got)
	}
}

func TestOriginMuxBlocksKeyMaterial(t *testing.T) {
	assetDir := t.TempDir()
	for _, name := range []string{"server.key", "server.crt"} {
		if err := os.WriteFile(filepath.Join(assetDir, name), []byte("secret"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	mux := newOriginMux(assetDir)
	req := httptest.NewRequest(http.MethodGet, "/server.key", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected key file to be hidden, got %d", rec.Code)
	}
}
