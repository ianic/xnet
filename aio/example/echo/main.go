package main

import (
	"log/slog"

	"github.com/ianic/xnet/aio"
	"github.com/ianic/xnet/aio/signal"
)

func main() {
	if err := run("127.0.0.1:4242"); err != nil {
		slog.Error("run", "error", err)
	}
}

func run(addr string) error {
	// start loop
	loop, err := aio.New(aio.Options{
		RingEntries:      128,
		RecvBuffersCount: 256,
		RecvBufferLen:    1024,
	})
	if err != nil {
		return err
	}
	defer loop.Close()

	// called when tcp listener accepts tcp connection
	tcpAccepted := func(fd int, tc *aio.TCPConn) {
		tc.Bind(&conn{fd: fd, sender: tc})
	}

	// start listener
	_, err = loop.Listen(addr, tcpAccepted)
	if err != nil {
		return err
	}
	slog.Debug("started server", "addr", addr)
	// run util interrupted
	ctx := signal.InterruptContext()
	if err := loop.Run(ctx); err != nil {
		return err
	}
	return nil
}

type Sender interface {
	Send(data []byte)
}

type conn struct {
	fd     int
	sender Sender
}

func (c *conn) Received(data []byte) {
	slog.Debug("received", "fd", c.fd, "len", len(data))
	dst := make([]byte, len(data))
	copy(dst, data)
	c.sender.Send(dst)
}

func (c *conn) Closed(error) {
	slog.Debug("closed", "fd", c.fd)
}
func (c *conn) Sent() {
	slog.Debug("sent", "fd", c.fd)
}
