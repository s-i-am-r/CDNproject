package main

import (
	"container/list"
	"crypto/tls"
	"flag"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go/http3"
)

type cacheEntry struct {
	key     string
	status  int
	header  http.Header
	body    []byte
	addedAt time.Time
	size    int64
}

type lruCache struct {
	mu         sync.Mutex
	maxEntries int
	maxBytes   int64
	usedBytes  int64
	ll         *list.List
	items      map[string]*list.Element
}

func newLRU(maxEntries int, maxBytes int64) *lruCache {
	return &lruCache{
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		ll:         list.New(),
		items:      make(map[string]*list.Element),
	}
}

func (c *lruCache) Get(key string) (*cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ele, ok := c.items[key]; ok {
		c.ll.MoveToFront(ele)
		return ele.Value.(*cacheEntry), true
	}
	return nil, false
}

func (c *lruCache) Add(e *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ele, ok := c.items[e.key]; ok {
		c.ll.MoveToFront(ele)
		prev := ele.Value.(*cacheEntry)
		c.usedBytes -= prev.size
		ele.Value = e
		c.usedBytes += e.size
	} else {
		ele := c.ll.PushFront(e)
		c.items[e.key] = ele
		c.usedBytes += e.size
	}

	for c.maxEntries > 0 && c.ll.Len() > c.maxEntries {
		c.removeOldest()
	}
	for c.maxBytes > 0 && c.usedBytes > c.maxBytes {
		c.removeOldest()
	}
}

func (c *lruCache) removeOldest() {
	ele := c.ll.Back()
	if ele == nil {
		return
	}
	c.ll.Remove(ele)
	e := ele.Value.(*cacheEntry)
	delete(c.items, e.key)
	c.usedBytes -= e.size
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		if strings.EqualFold(k, "Connection") ||
			strings.EqualFold(k, "Proxy-Connection") ||
			strings.EqualFold(k, "Keep-Alive") ||
			strings.EqualFold(k, "Proxy-Authenticate") ||
			strings.EqualFold(k, "Proxy-Authorization") ||
			strings.EqualFold(k, "Te") ||
			strings.EqualFold(k, "Trailer") ||
			strings.EqualFold(k, "Transfer-Encoding") ||
			strings.EqualFold(k, "Upgrade") {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func main() {
	var (
		addr          = flag.String("addr", ":8443", "listen address for CDN")
		origin        = flag.String("origin", "https://localhost:443", "origin base URL")
		certFile      = flag.String("cert", "server.crt", "TLS cert file")
		keyFile       = flag.String("key", "server.key", "TLS key file")
		maxEntries    = flag.Int("max-entries", 1024, "maximum cache entries (0 = unlimited)")
		maxBytes      = flag.Int64("max-bytes", 128<<20, "maximum cache bytes (0 = unlimited)")
		maxEntryBytes = flag.Int64("max-entry-bytes", 16<<20, "maximum size for a cached entry")
	)
	flag.Parse()

	originURL, err := url.Parse(*origin)
	if err != nil {
		log.Fatalf("invalid origin: %v", err)
	}
	if originURL.Scheme != "https" {
		log.Fatalf("origin must be https for http3, got %q", originURL.Scheme)
	}

	cache := newLRU(*maxEntries, *maxBytes)

	originRT := &http3.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: true, // origin is local/self-signed
			NextProtos:         []string{"h3"},
		},
	}
	defer func() {
		if err := originRT.Close(); err != nil {
			log.Printf("origin http3 close error: %v", err)
		}
	}()
	client := &http.Client{Transport: originRT}
	handler := newCDNHandler(client, originURL, cache, *maxEntryBytes)

	h3 := &http3.Server{
		Addr:    *addr,
		Handler: accessLog(handler),
	}

	hh := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := h3.SetQUICHeaders(w.Header()); err != nil {
				log.Println(err)
			}
			next.ServeHTTP(w, r)
		})
	}

	server := &http.Server{
		Addr:      *addr,
		Handler:   hh(accessLog(handler)),
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, NextProtos: []string{"h3"}},
	}

	log.Printf("cdn listening on %s (origin %s)", *addr, originURL.String())
	go func() {
		log.Fatal(h3.ListenAndServeTLS(*certFile, *keyFile))
	}()
	log.Fatal(server.ListenAndServeTLS(*certFile, *keyFile))
}

func newCDNHandler(client *http.Client, originURL *url.URL, cache *lruCache, maxEntryBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			proxyPass(w, r, client, originURL)
			return
		}
		if r.Header.Get("Range") != "" {
			proxyPass(w, r, client, originURL)
			return
		}

		key := r.URL.RequestURI()
		if e, ok := cache.Get(key); ok {
			copyHeaders(w.Header(), e.header)
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(e.status)
			if r.Method == http.MethodGet {
				_, _ = w.Write(e.body)
			}
			return
		}

		originReq, err := http.NewRequestWithContext(r.Context(), r.Method, originURL.ResolveReference(r.URL).String(), nil)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		copyHeaders(originReq.Header, r.Header)

		resp, err := client.Do(originReq)
		if err != nil {
			http.Error(w, "origin unavailable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			copyHeaders(w.Header(), resp.Header)
			w.Header().Set("X-Cache", "MISS")
			w.WriteHeader(resp.StatusCode)
			if r.Method == http.MethodGet {
				_, _ = io.Copy(w, resp.Body)
			}
			return
		}

		contentLength := int64(-1)
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			if v, err := strconv.ParseInt(cl, 10, 64); err == nil {
				contentLength = v
			}
		}

		if contentLength < 0 || contentLength > maxEntryBytes {
			copyHeaders(w.Header(), resp.Header)
			w.Header().Set("X-Cache", "MISS")
			w.WriteHeader(resp.StatusCode)
			if r.Method == http.MethodGet {
				_, _ = io.Copy(w, resp.Body)
			}
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "origin read error", http.StatusBadGateway)
			return
		}

		entry := &cacheEntry{
			key:     key,
			status:  resp.StatusCode,
			header:  resp.Header.Clone(),
			body:    body,
			addedAt: time.Now(),
			size:    int64(len(body)),
		}
		cache.Add(entry)

		copyHeaders(w.Header(), entry.header)
		w.Header().Set("X-Cache", "MISS")
		w.WriteHeader(entry.status)
		if r.Method == http.MethodGet {
			_, _ = w.Write(entry.body)
		}
	})
}

func proxyPass(w http.ResponseWriter, r *http.Request, client *http.Client, originURL *url.URL) {
	originReq, err := http.NewRequestWithContext(r.Context(), r.Method, originURL.ResolveReference(r.URL).String(), r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	copyHeaders(originReq.Header, r.Header)

	resp, err := client.Do(originReq)
	if err != nil {
		http.Error(w, "origin unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("X-Cache", "MISS")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.status = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(p []byte) (int, error) {
	if lrw.status == 0 {
		lrw.status = http.StatusOK
	}
	n, err := lrw.ResponseWriter.Write(p)
	lrw.bytes += n
	return n, err
}

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(lrw, r)
		if lrw.status == 0 {
			lrw.status = http.StatusOK
		}
		latency := time.Since(start)
		remote := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			remote = forwarded
		} else if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			remote = realIP
		}
		xcache := lrw.Header().Get("X-Cache")
		if xcache == "" {
			xcache = "-"
		}
		log.Printf("cdn-access method=%s path=%s status=%d bytes=%d latency=%s remote=%s xcache=%s ua=%q",
			r.Method, r.URL.Path, lrw.status, lrw.bytes, latency, remote, xcache, r.UserAgent(),
		)
	})
}
