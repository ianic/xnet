package ws

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
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

func NewFromRequest(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	h, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("Response Writer don't support Hijacker interface")
	}
	nc, brw, err := h.Hijack()
	if err != nil {
		return nil, err
	}
	hs, err := NewHandshakeFromRequest(r)
	if err != nil {
		return nil, err
	}
	_, err = nc.Write([]byte(hs.Response()))
	if err != nil {
		return nil, err
	}
	ws := NewConnection(nc, brw.Reader, hs.extension.permessageDeflate)
	return &ws, nil
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

// New creates new WebSocket connection from raw tcp connection.
// Reads http upgrade request from client and sends response.
func New(nc net.Conn) (*Conn, error) {
	br := bufio.NewReader(deadlineReader{nc: nc})
	hs, err := NewHandshake(br)
	if err != nil {
		return nil, err
	}
	_, err = nc.Write([]byte(hs.Response()))
	if err != nil {
		return nil, err
	}
	ws := NewConnection(nc, br, hs.extension.permessageDeflate)
	return &ws, nil
}
