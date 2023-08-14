package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"
)

func main() {
	address := "localhost:3333"
	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	defer listener.Close()
	fmt.Println("Listening on ", address)
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		go handleRequest(conn)
	}
}

// Handles incoming requests.
func handleRequest(conn net.Conn) {
	defer conn.Close()

	ws, err := handshake(conn)
	if err != nil {
		log.Printf("handshake failed %s", err)
		return
	}

	for {
		msg, err := ws.Read()
		if err != nil {
			log.Printf("connection closed %s", err)
			return
		}
		fmt.Printf("%s", string(msg.Payload))
		if err := msg.SendTo(conn); err != nil {
			log.Printf("msg send error %s", err)
			return
		}
	}
}

func handshake(conn net.Conn) (*Connection, error) {
	deadline := time.Now().Add(time.Second * 15)
	conn.SetReadDeadline(deadline)

	pos := 0
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf[pos:])
		if err != nil {
			return nil, err
		}
		pos += n
		if bytes.HasSuffix(buf[:pos], []byte(requestEnd)) {
			hs, err := Parse(buf[:pos])
			if err != nil {
				return nil, err
			}
			_, err = conn.Write([]byte(hs.Response()))
			if err != nil {
				return nil, err
			}

			conn.SetReadDeadline(time.Time{})
			ws := NewConnection(conn)
			return &ws, nil
		}
		pos += n
		if pos == len(buf) {
			return nil, errors.New("request header not found")
		}
	}
}
