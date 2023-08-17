package ws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"time"
)

func Serve(ctx context.Context, address string, handler func(*Conn)) error {
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
		nc, err := nl.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				return err
			}
			break
		}
		go func(nc net.Conn) {
			defer nc.Close()
			ws, err := handshake(nc)
			if err != nil {
				return
			}
			handler(ws)
		}(nc)
	}
	return nil
}

func Echo(ws *Conn) {
	for {
		opcode, payload, err := ws.Read()
		if err != nil {
			if err != io.EOF {
				log.Printf("read error %s", err)
			}
			return
		}
		// fmt.Printf("%s", string(msg.Payload))
		if err := ws.Write(opcode, payload); err != nil {
			log.Printf("write error %s", err)
			return
		}
	}
}

func handshake(conn net.Conn) (*Conn, error) {
	conn.SetReadDeadline(fromTimeout(15 * time.Second))

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

			conn.SetReadDeadline(resetDeadline)
			ws := NewConnection(conn, hs.extension.permessageDeflate)
			return &ws, nil
		}
		pos += n
		if pos == len(buf) {
			return nil, errors.New("request header not found")
		}
	}
}
