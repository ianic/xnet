package main

import (
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/ianic/xnet/aio"
	"github.com/ianic/xnet/aio/signal"
	"github.com/ianic/xnet/ws"
)

func main() {
	if err := run(4243); err != nil {
		log.Panic(err)
	}
}

func run(port int) error {
	// start loop
	loop, err := aio.New(aio.DefaultOptions)
	if err != nil {
		return err
	}
	defer loop.Close()

	// start listener
	lsn, err := aio.NewTcpListener(loop, port, func(fd int, tc *aio.TcpConn) aio.Conn {
		return &handshake{fd: fd, tcpConn: tc}
	})
	if err != nil {
		return err
	}

	// run loop until interrupt
	ctx := signal.InteruptContext()
	if err := loop.Run(ctx, time.Second); err != nil {
		slog.Error("run", "error", err)
	}
	// stop listener
	lsn.Close()
	// run loop until all connections closes
	if err := loop.RunUntilDone(); err != nil {
		slog.Error("run", "error", err)
	}

	return nil
}

type handshake struct {
	fd      int
	tcpConn *aio.TcpConn
	hs      ws.Handshake
}

func (h *handshake) Received(data []byte) {
	var err error
	h.hs, err = ws.NewHandshakeFromBuffer(data)
	if err != nil {
		slog.Info("handshake failed", slog.String("error", err.Error()))
		h.tcpConn.Close()
		return
	}
	h.tcpConn.Send([]byte(h.hs.Response()))
}
func (h *handshake) Closed(error) {}
func (h *handshake) Sent(error) {
	// link websocket connection as upper layer of TcpConn instead of handshake
	u := &upstream{}
	ac := h.hs.NewAsyncConn(h.tcpConn, u)
	h.tcpConn.SetConn(ac)
}

type upstream struct{}

func (u *upstream) Received(data []byte) {
	fmt.Printf("%s", data)
}
func (u *upstream) Closed(error) {
	fmt.Printf("closed\n")
}
func (u *upstream) Sent(error) {}
