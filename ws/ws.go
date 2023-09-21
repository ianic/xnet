package ws

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"
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

type SessionHandler interface {
	OnConnect(int)
	OnDisconnect(int)
	OnData(int, []byte)
}
type Session interface {
	Send(payload []byte) error
}

type Upgrader struct {
	handler SessionHandler
	conns   map[int]*Conn
	nextID  int
	sync.Mutex
}

func NewUpgrader(handler SessionHandler) *Upgrader {
	return &Upgrader{
		handler: handler,
		conns:   make(map[int]*Conn),
	}
}

func (u *Upgrader) Upgrade(w http.ResponseWriter, r *http.Request) {
	wc, err := NewFromRequest(w, r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	u.Lock()
	u.nextID++
	id := u.nextID
	u.conns[id] = wc
	u.Unlock()
	go u.readLoop(id, wc)
}

func (u *Upgrader) readLoop(id int, wc *Conn) {
	u.handler.OnConnect(id)
	for {
		_, payload, err := wc.Read()
		if err != nil {
			break
		}
		u.handler.OnData(id, payload)
	}
	u.handler.OnDisconnect(id)
	u.Lock()
	delete(u.conns, id)
	u.Unlock()
}

func (u *Upgrader) Send(id int, data []byte) error {
	u.Lock()
	wc, ok := u.conns[id]
	u.Unlock()
	if ok {
		return wc.WriteBinary(data)
	}
	return errors.New("connection not found")
}

func (u *Upgrader) Shutdown(ctx context.Context) error {
	u.Lock()
	for _, wc := range u.conns {
		wc.Close()
	}
	u.Unlock()

	// Code copied from net/http/server.go:Server.Shutdown
	// Waiting for readLoops to finish.
	const shutdownPollIntervalMax = 500 * time.Millisecond
	pollIntervalBase := time.Millisecond
	nextPollInterval := func() time.Duration {
		// Add 10% jitter.
		interval := pollIntervalBase + time.Duration(rand.Intn(int(pollIntervalBase/10)))
		// Double and clamp for next time.
		pollIntervalBase *= 2
		if pollIntervalBase > shutdownPollIntervalMax {
			pollIntervalBase = shutdownPollIntervalMax
		}
		return interval
	}

	timer := time.NewTimer(nextPollInterval())
	defer timer.Stop()
	for {
		u.Lock()
		if len(u.conns) == 0 {
			u.Unlock()
			return nil
		}
		u.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			timer.Reset(nextPollInterval())
		}
	}
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

func Client(nc net.Conn, host, path, origin string) (*Conn, error) {
	secKey, err := secKey()
	if err != nil {
		return nil, err
	}
	format := "GET %s HTTP/1.1\r\n" +
		"Host: %s\r\n" +
		"Origin: %s\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: %s\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"

	upgradeReq := fmt.Sprintf(format, path, host, origin, secKey)
	fmt.Printf("%s", upgradeReq)
	_, err = nc.Write([]byte(upgradeReq))
	if err != nil {
		return nil, err
	}

	br := bufio.NewReader(deadlineReader{nc: nc})

	req, err := http.ReadResponse(br, nil)
	if err != nil {
		return nil, err
	}
	secAccept := secAccept(secKey)
	secAcceptActual := req.Header.Get("Sec-WebSocket-Accept")
	if secAcceptActual != secAccept {
		return nil, fmt.Errorf("wrong accept key")
	}

	ws := NewConnection(nc, br, false)
	return &ws, nil
}
