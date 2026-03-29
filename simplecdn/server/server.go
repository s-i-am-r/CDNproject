package main

import (
	"crypto/tls"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/quic-go/quic-go/http3"
)

type neuteredFileSystem struct {
	fs      http.FileSystem
	blocked map[string]struct{}
}

func (nfs neuteredFileSystem) Open(name string) (http.File, error) {
	base := path.Base(path.Clean(name))
	if _, ok := nfs.blocked[base]; ok {
		return nil, os.ErrNotExist
	}

	f, err := nfs.fs.Open(name)
	if err != nil {
		return nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if stat.IsDir() {
		index := path.Join(name, "index.html")
		if _, err := nfs.fs.Open(index); err != nil {
			return nil, os.ErrNotExist
		}
	}

	return f, nil
}

func main() {
	logged := accessLog(newOriginMux("."))

	h3 := &http3.Server{
		Addr:    ":443",
		Handler: logged,
	}

	hh := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			err := h3.SetQUICHeaders(w.Header())
			if err != nil {
				log.Println(err)
			}
			next.ServeHTTP(w, r)
		})
	}

	certFile := "server.crt"
	keyFile := "server.key"

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h3"}, // ⭐ REQUIRED
	}

	tcpServer := &http.Server{
		Addr:      ":443",
		Handler:   hh(logged),
		TLSConfig: tlsConfig,
	}

	go func() {
		log.Fatal(h3.ListenAndServeTLS(certFile, keyFile))
	}()

	log.Fatal(tcpServer.ListenAndServeTLS(certFile, keyFile))
}

func newOriginMux(staticDir string) *http.ServeMux {
	_ = mime.AddExtensionType(".m3u8", "application/vnd.apple.mpegurl")
	_ = mime.AddExtensionType(".ts", "video/mp2t")

	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	})
	fileServer := http.FileServer(neuteredFileSystem{
		fs: http.Dir(staticDir),
		blocked: map[string]struct{}{
			"server.key": {},
			"server.crt": {},
		},
	})
	mux.Handle("/", fileServer)
	return mux
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
		log.Printf("access method=%s path=%s status=%d bytes=%d latency=%s remote=%s ua=%q",
			r.Method, r.URL.Path, lrw.status, lrw.bytes, latency, remote, r.UserAgent(),
		)
	})
}
