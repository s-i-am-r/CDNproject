package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	quic "github.com/quic-go/quic-go"
)

func main() {
	// ---- ENV CONFIG ----
	host := os.Getenv("QUIC_HOST")
	port := os.Getenv("QUIC_PORT")

	if host == "" || port == "" {
		log.Print("Set QUIC_HOST and QUIC_PORT")
	}

	addr := fmt.Sprintf("[%s]:%s", host, port)

	// // Optional delay before reading response
	// waitStr := os.Getenv("WAIT_BEFORE_READ") // seconds
	// waitSec := 0
	// if waitStr != "" {
	// 	var err error
	// 	waitSec, err = strconv.Atoi(waitStr)
	// 	if err != nil {
	// 		log.Print("Invalid WAIT_BEFORE_READ")
	// 	}
	// }

	// ---- TLS CONFIG ----
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-example"},
	}

	quicConf := &quic.Config{
		EnableDatagrams: true,
		MaxIdleTimeout:  10 * time.Minute,
	}

	// ---- CONNECT ----
	conn, err := quic.DialAddr(context.Background(), addr, tlsConf, quicConf)
	if err != nil {
		log.Print(err)
	}
	defer conn.CloseWithError(0, "done")

	fmt.Println("Connected to", addr)

	// ---- READ MESSAGE FROM STDIN ----
	fmt.Print("Enter message: ")
	reader := bufio.NewReader(os.Stdin)

	// ---- OPEN STREAM ----
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Print(err)
	}
	defer stream.Close()
	for {

		message, err := reader.ReadString('\n')
		if err != nil {
			log.Print(err)
			break
		}
		message = strings.TrimSpace(message)
		// ---- SEND ----
		_, err = stream.Write([]byte(message))
		if err != nil {
			log.Print(err)
			break
		}

		fmt.Println("Sent:", message)

		// // ---- OPTIONAL WAIT ----
		// if waitSec > 0 {
		// 	fmt.Printf("Waiting %d seconds before reading...\n", waitSec)
		// 	time.Sleep(time.Duration(waitSec) * time.Second)
		// }

		// ---- RECEIVE ----
		buf := make([]byte, 1024)
		n, err := stream.Read(buf)
		if err != nil {
			log.Print(err)
			break
		}

		fmt.Println("Received:", string(buf[:n]))

	}

}
