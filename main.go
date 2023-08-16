package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// InteruptContext returns context which will be closed on application interupt
func InteruptContext() context.Context {
	ctx, stop := context.WithCancel(context.Background())
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		stop()
	}()
	return ctx
}

func main() {
	address := "localhost:9001"

	ctx := InteruptContext()
	if err := Serve(ctx, address, echo); err != nil {
		log.Fatal(err)
	}
}

func Serve(ctx context.Context, address string, handler func(net.Conn)) error {
	nl, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		nl.Close()
	}()
	// fmt.Println("Listening on ", address)
	for {
		conn, err := nl.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				return err
			}
			break
		}
		go handler(conn)
	}
	return nil
}

func echo(nc net.Conn) {
	defer nc.Close()
	ws, err := handshake(nc)
	if err != nil {
		log.Printf("handshake failed %s", err)
		return
	}

	for {
		msg, err := ws.Read()
		if err != nil {
			if err != io.EOF {
				log.Printf("connection closed %s", err)
			}
			return
		}
		// fmt.Printf("%s", string(msg.Payload))
		if err := ws.Write(msg); err != nil {
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
			hs, err := NewHandshake(buf[:pos])
			if err != nil {
				return nil, err
			}
			_, err = conn.Write([]byte(hs.Response()))
			if err != nil {
				return nil, err
			}

			conn.SetReadDeadline(time.Time{})
			ws := NewConnection(conn, hs.extension)
			return &ws, nil
		}
		pos += n
		if pos == len(buf) {
			return nil, errors.New("request header not found")
		}
	}
}
