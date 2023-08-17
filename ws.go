package ws

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
)

// Serve starts WebSocket server listening at `address`.
// connectHandler is called with each established WebSocket connection. It's up
// to connectHandler to read/write/close that connection.
func Serve(ctx context.Context, address string, connectHandler func(*Conn)) error {
	nl, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		nl.Close()
	}()
	for {
		nc, err := nl.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				return err
			}
			break
		}
		go func(nc net.Conn) {
			ws, err := New(nc)
			if err != nil {
				nc.Close()
				return
			}
			connectHandler(ws)
		}(nc)
	}
	return nil
}

// Echo all websocket messages received from client to the same client.
func Echo(ws *Conn) {
	for {
		opcode, payload, err := ws.Read()
		if err != nil {
			if err != io.EOF {
				log.Printf("read error %s", err)
			}
			return
		}
		if err := ws.Write(opcode, payload); err != nil {
			log.Printf("write error %s", err)
			return
		}
	}
}
