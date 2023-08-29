package aio

import (
	"log/slog"
	"syscall"
)

type NewConn func(int, *TcpConn) Upstream

type TcpListener struct {
	loop    *Loop
	fd      int
	newConn NewConn
	conns   map[int]*TcpConn
}

func NewTcpListener(loop *Loop, port int, newConn NewConn) (*TcpListener, error) {
	l := &TcpListener{
		loop:    loop,
		newConn: newConn,
		conns:   make(map[int]*TcpConn),
	}
	if err := l.bind(port); err != nil {
		return nil, err
	}
	l.accept()
	return l, nil
}

func (l *TcpListener) accept() {
	l.loop.PrepareMultishotAccept(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
		if errno == 0 {
			fd := int(res)
			// connect Conn and TcpConn
			tc := &TcpConn{loop: l.loop, fd: fd, lsn: l}
			conn := l.newConn(fd, tc)
			tc.up = conn
			l.conns[fd] = tc
			tc.recvLoop()
			return
		}
		if errno != syscall.ECANCELED {
			slog.Debug("listener accept", "fd", l.fd, "errno", errno, "res", res, "flags", flags)
		}
	})
}

func (l *TcpListener) bind(port int) error {
	addr := syscall.SockaddrInet4{Port: port}
	fd, err := bind(&addr)
	if err != nil {
		return err
	}
	l.fd = fd
	return nil
}

func (l *TcpListener) Close() {
	l.loop.PrepareCancelFd(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
		if errno != 0 {
			slog.Debug("listener cancel", "fd", l.fd, "errno", errno, "res", res, "flags", flags)
		}
		for _, conn := range l.conns {
			conn.shutdown(ErrListenerClose)
		}
	})
}

func (l *TcpListener) ConnCount() int {
	return len(l.conns)
}

func (l *TcpListener) remove(fd int) {
	delete(l.conns, fd)
}
