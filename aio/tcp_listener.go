package aio

import (
	"log/slog"
	"syscall"
)

type SenderCloser interface {
	Send(data []byte) error
	Close()
}

type Conn interface {
	Received([]byte)
	Closed(error)
	Sent(error)
}

type tcpConn struct {
	lsn  *TcpListener
	loop *Loop
	fd   int
	conn Conn
}

func (l *tcpConn) Send(data []byte) error {
	nn := 0
	var cb completionCallback
	cb = func(res int32, flags uint32, errno syscall.Errno) {
		nn += int(res) // bytes written so far
		if errno > 0 {
			// TODO: here nn could be > 0
			// usually senders report that situation, but I don't see usage of that
			l.conn.Sent(errno)
			return
		}
		if nn >= len(data) {
			// all sent call callback
			l.conn.Sent(nil)
			return
		}
		if err := l.loop.PrepareSend(l.fd, data[nn:], cb); err != nil {
			// here also nn could be > 0
			l.conn.Sent(err)
		}
		// new send prepared
	}
	return l.loop.PrepareSend(l.fd, data, cb)
}

func (l *tcpConn) Close() {
	l.shutdown(nil)
}

// recvLoop starts multishot recv on fd
// Will receive on fd until error occures.
func (l *tcpConn) recvLoop() error {
	var cb completionCallback
	cb = func(res int32, flags uint32, errno syscall.Errno) {
		if errno > 0 {
			if TemporaryErrno(errno) {
				slog.Debug("read temporary error", "error", errno.Error(), "errno", uint(errno), "flags", flags)
				l.loop.PrepareRecv(l.fd, cb)
				return
			}
			if errno != syscall.ECONNRESET {
				slog.Warn("read error", "error", errno.Error(), "errno", uint(errno), "flags", flags)
			}
			l.shutdown(errno)
			return
		}
		if res == 0 {
			l.shutdown(nil)
			return
		}
		buf, id := l.loop.buffers.get(res, flags)
		//slog.Debug("read", "bufferID", id, "len", len(buf))
		l.conn.Received(buf)
		l.loop.buffers.release(buf, id)
	}
	return l.loop.PrepareRecv(l.fd, cb)
}

func (l *tcpConn) shutdown(err error) {
	l.loop.PrepareShutdown(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
		if !(errno == 0 || errno == syscall.ENOTCONN) {
			slog.Debug("shutdown", "fd", l.fd, "errno", errno, "res", res, "flags", flags)
		}
		if errno != 0 {
			l.lsn.remove(l.fd)
			return
		}
		l.loop.PrepareClose(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
			l.lsn.remove(l.fd)
			if errno != 0 {
				slog.Debug("close", "fd", l.fd, "errno", errno, "res", res, "flags", flags)
				l.conn.Closed(errno)
				return
			}
			l.conn.Closed(nil)
		})
	})
}

type NewConn func(int, SenderCloser) Conn

type TcpListener struct {
	loop    *Loop
	fd      int
	newConn NewConn
	conns   map[int]tcpConn
}

func NewTcpListener(loop *Loop, port int, newConn NewConn) (*TcpListener, error) {
	l := &TcpListener{
		loop:    loop,
		newConn: newConn,
		conns:   make(map[int]tcpConn),
	}
	if err := l.bind(port); err != nil {
		return nil, err
	}
	if err := l.accept(); err != nil {
		syscall.Close(l.fd)
		return nil, err
	}
	return l, nil
}

func (l *TcpListener) accept() error {
	return l.loop.PrepareMultishotAccept(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
		if errno == 0 {
			fd := int(res)
			// connect conn and tcpConn
			tc := tcpConn{loop: l.loop, fd: fd, lsn: l}
			conn := l.newConn(fd, &tc)
			tc.conn = conn
			l.conns[fd] = tc
			tc.recvLoop()
			return
		}
		if errno != syscall.ECANCELED {
			slog.Debug("accept", "errno", errno, "res", res, "flags", flags)
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

func (l *TcpListener) Close() error {
	return l.loop.PrepareCancelFd(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
		//slog.Debug("TcpListener close", "errno", errno, "res", res, "flags", flags)
		for _, conn := range l.conns {
			conn.shutdown(nil)
		}
		//clear(l.conns)
	})
}

func (l *TcpListener) ConnCount() int {
	return len(l.conns)
}

func (l *TcpListener) remove(fd int) {
	delete(l.conns, fd)
}
