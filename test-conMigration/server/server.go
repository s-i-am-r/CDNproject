package main

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

func main() {

	tlsConf := generateTLSConfig()

	quicConf := &quic.Config{
		EnableDatagrams: true,
		MaxIdleTimeout:  10 * time.Minute,
	}

	listener, err := quic.ListenAddr("[::]:4242", tlsConf, quicConf)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("QUIC server listening on :4242")

	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			log.Println(err)
			continue
		}

		go handleConn(conn)
	}
}

func handleConn(conn *quic.Conn) {

	log.Println("New connection from:", conn.RemoteAddr())
	var closed atomic.Bool
	closed.Store(false)
	go monitorMigration(conn, &closed)

	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			log.Print("cloding!!\n")
			closed.Store(true)
			return
		}

		go handleStream(stream)
	}
}

func monitorMigration(conn *quic.Conn, closed *atomic.Bool) {

	prev := conn.RemoteAddr().String()

	for {
		if closed.Load() {
			break
		}
		time.Sleep(time.Second)

		cur := conn.RemoteAddr().String()
		log.Println(" addr:", cur)

		if cur != prev {
			log.Println("Connection migrated:")
			log.Println(" old:", prev)
			log.Println(" new:", cur)

			prev = cur
		}
	}
}

func handleStream(stream *quic.Stream) {

	buf := make([]byte, 256)
	prev_buf := make([]byte, 256)
	copy(prev_buf, "hello")

	for {
		for i := range buf {
			buf[i] = 0
		}
		n, err := stream.Read(buf)
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Println(err)
			return
		}
		log.Printf("received: %s\n", string(buf[:n]))
		stream.Write(prev_buf)
		// clear it (set all bytes to 0)
		for i := range prev_buf {
			prev_buf[i] = 0
		}
		copy(prev_buf, buf)

	}
}

func generateTLSConfig() *tls.Config {

	cert, err := tls.LoadX509KeyPair("cert.pem", "key.pem")
	if err != nil {
		log.Fatal(err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-example"},
	}
}
