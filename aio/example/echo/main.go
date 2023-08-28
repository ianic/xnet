package main

import (
	"log/slog"
	"time"

	"github.com/ianic/iol"
	"github.com/ianic/iol/signal"
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
	lp, err := iol.New(iol.Options{
		RingEntries:      128,
		RecvBuffersCount: 256,
		RecvBufferLen:    1024,
	})
	if err != nil {
		return err
	}
	defer lp.Close()
	srv := server{}
	ln, err := iol.NewListener(lp, port, &srv)
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
	sender iol.Sender
}

func (s *server) OnStart(writer iol.Sender) {
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
