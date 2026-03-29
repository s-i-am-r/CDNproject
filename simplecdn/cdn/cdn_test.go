package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestCDNHandlerCachesGETResponses(t *testing.T) {
	var originCalls atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			originCalls.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Length": []string{"11"},
					"Content-Type":   []string{"text/plain; charset=utf-8"},
				},
				Body:          io.NopCloser(bytes.NewBufferString("hello world")),
				ContentLength: 11,
			}, nil
		}),
	}

	originURL := &url.URL{Scheme: "https", Host: "origin.test"}
	handler := newCDNHandler(client, originURL, newLRU(16, 1<<20), 1<<20)

	first := httptest.NewRequest(http.MethodGet, "https://cdn.test/hello", nil)
	firstRec := httptest.NewRecorder()
	handler.ServeHTTP(firstRec, first)

	if got := firstRec.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected first request to be a cache miss, got %q", got)
	}

	second := httptest.NewRequest(http.MethodGet, "https://cdn.test/hello", nil)
	secondRec := httptest.NewRecorder()
	handler.ServeHTTP(secondRec, second)

	if got := secondRec.Header().Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected second request to be a cache hit, got %q", got)
	}
	if got := secondRec.Body.String(); got != "hello world" {
		t.Fatalf("unexpected cached body %q", got)
	}
	if got := originCalls.Load(); got != 1 {
		t.Fatalf("expected one origin request, got %d", got)
	}
}

func TestCDNHandlerBypassesCacheForRangeRequests(t *testing.T) {
	var originCalls atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			originCalls.Add(1)
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Header: http.Header{
					"Content-Length": []string{"4"},
					"Content-Range":  []string{"bytes 0-3/10"},
				},
				Body:          io.NopCloser(bytes.NewBufferString("test")),
				ContentLength: 4,
			}, nil
		}),
	}

	originURL := &url.URL{Scheme: "https", Host: "origin.test"}
	handler := newCDNHandler(client, originURL, newLRU(16, 1<<20), 1<<20)

	for range 2 {
		req := httptest.NewRequest(http.MethodGet, "https://cdn.test/video/segment0.ts", nil)
		req.Header.Set("Range", "bytes=0-3")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("X-Cache"); got != "MISS" {
			t.Fatalf("expected range request miss, got %q", got)
		}
	}

	if got := originCalls.Load(); got != 2 {
		t.Fatalf("expected two origin requests for ranged fetches, got %d", got)
	}
}

func TestCDNHandlerCachesHLSPlaylist(t *testing.T) {
	var originCalls atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			originCalls.Add(1)
			playlist := "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:2.0,\nsegment0.ts\n"
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Length": []string{"52"},
					"Content-Type":   []string{"application/vnd.apple.mpegurl"},
				},
				Body:          io.NopCloser(bytes.NewBufferString(playlist)),
				ContentLength: int64(len(playlist)),
			}, nil
		}),
	}

	originURL := &url.URL{Scheme: "https", Host: "origin.test"}
	handler := newCDNHandler(client, originURL, newLRU(16, 1<<20), 1<<20)

	req := httptest.NewRequest(http.MethodGet, "https://cdn.test/hls/stream.m3u8", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); got != "application/vnd.apple.mpegurl" {
		t.Fatalf("unexpected playlist content type %q", got)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "https://cdn.test/hls/stream.m3u8", nil))
	if got := rec2.Header().Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected cached playlist HIT, got %q", got)
	}
	if got := originCalls.Load(); got != 1 {
		t.Fatalf("expected one origin request for playlist, got %d", got)
	}
}
