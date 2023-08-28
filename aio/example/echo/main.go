package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ianic/xnet/aio"
	"github.com/ianic/xnet/aio/signal"
)

func main() {
	// slog.SetDefault(slog.New(
	// 	slog.NewTextHandler(
	// 		os.Stderr,
	// 		&slog.HandlerOptions{
	// 			Level:     slog.LevelDebug,
	// 			AddSource: true,
	// 		})))
	if err := run(4242); err != nil {
		slog.Error("run", "error", err)
	}

}

func run(port int) error {
	slog.Debug("starting server", "port", port)
	lp, err := aio.New(aio.Options{
		RingEntries:      128,
		RecvBuffersCount: 256,
		RecvBufferLen:    1024,
	})
	if err != nil {
		return err
	}
	defer lp.Close()
	ln, err := aio.NewTcpListener(lp, port, func(fd int, senderCloser aio.SenderCloser) aio.Conn {
		return &conn{fd: fd, sender: senderCloser}
	})
	if err != nil {
		return err
	}

	ctx := signal.InteruptContext()
	if err := lp.Run(ctx, time.Second); err != nil {
		slog.Error("run", "error", err)
	}
	ln.Close()
	if err := lp.RunUntilDone(); err != nil {
		slog.Error("run", "error", err)
	}
	if cc := ln.ConnCount(); cc != 0 {
		panic(fmt.Sprintf("listner conn count should be 0 actual %d", cc))
	}
	return nil
}

type Sender interface {
	Send(data []byte) error
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
func (c *conn) Sent(error) {
	slog.Debug("sent", "fd", c.fd)
}
