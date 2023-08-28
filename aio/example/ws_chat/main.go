package main

import (
	"log"
	"log/slog"
	"time"

	"github.com/ianic/xnet/aio"
	"github.com/ianic/xnet/aio/signal"
)

func main() {
	if err := run(4243); err != nil {
		log.Panic(err)
	}

}

func run(port int) error {
	slog.Debug("starting server", "port", port)
	lp, err := aio.New(aio.DefaultOptions)
	if err != nil {
		return err
	}
	defer lp.Close()
	srv := server{}
	ln, err := aio.NewListener(lp, port, &srv)
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

	return nil
}

type server struct {
	sender aio.Sender
}

func (s *server) OnStart(writer aio.Sender) {
	s.sender = writer
}

func (s *server) OnConnect(fd int) {
	slog.Debug("connect", "fd", fd)
}

func (s *server) OnDisconnect(fd int, err error) {
	slog.Debug("disconnect", "fd", fd)
}

func (s *server) OnRecv(fd int, data []byte) {
	slog.Debug("received", "fd", fd, "len", len(data))

	dst := make([]byte, len(data))
	copy(dst, data)
	s.sender.Send(fd, dst)
}

func (s *server) OnSend(fd int, err error) {
	slog.Debug("sent", "fd", fd)
}
