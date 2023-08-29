package aio

import (
	"errors"
	"io"
	"log/slog"
	"syscall"
)

// upper layer's events handler interface
type Upstream interface {
	Received([]byte)
	Closed(error)
	Sent()
}

type TcpConn struct {
	lsn           *TcpListener
	loop          *Loop
	fd            int
	up            Upstream
	shutdownError error
}

var (
	ErrListenerClose = errors.New("listener closed connection")
	ErrUpstreamClose = errors.New("upstream closed connection")
)

// TODO: add correlation id (userdata) for send/sent connecting
func (l *TcpConn) Send(data []byte) {
	nn := 0 // number of bytes sent
	var cb completionCallback
	cb = func(res int32, flags uint32, errno syscall.Errno) {
		nn += int(res) // bytes written so far
		if errno > 0 {
			l.shutdown(errno)
			return
		}
		if nn >= len(data) {
			// all sent call callback
			l.up.Sent()
			return
		}
		// send rest of the data
		l.loop.PrepareSend(l.fd, data[nn:], cb)
		// new send prepared
	}
	l.loop.PrepareSend(l.fd, data, cb)
}

func (l *TcpConn) Close() {
	l.shutdown(ErrUpstreamClose)
}

// Allows upper layer to change connection handler.
// Useful in websocket handshake for example.
func (l *TcpConn) SetUpstream(conn Upstream) {
	l.up = conn
}

// recvLoop starts multishot recv on fd
// Will receive on fd until error occurs.
func (l *TcpConn) recvLoop() {
	var cb completionCallback
	cb = func(res int32, flags uint32, errno syscall.Errno) {
		if errno > 0 {
			if TemporaryErrno(errno) {
				slog.Debug("tcp conn read temporary error", "error", errno.Error(), "errno", uint(errno), "flags", flags)
				l.loop.PrepareRecv(l.fd, cb)
				return
			}
			if errno != syscall.ECONNRESET {
				slog.Warn("tcp conn read error", "error", errno.Error(), "errno", uint(errno), "flags", flags)
			}
			l.shutdown(errno)
			return
		}
		if res == 0 {
			l.shutdown(io.EOF)
			return
		}
		buf, id := l.loop.buffers.get(res, flags)
		l.up.Received(buf)
		l.loop.buffers.release(buf, id)
		if !isMultiShot(flags) {
			slog.Debug("tcp conn multishot terminated", slog.Uint64("flags", uint64(flags)), slog.Uint64("errno", uint64(errno)))
			// io_uring can terminate multishot recv when cqe is full
			// need to restart it then
			// ref: https://lore.kernel.org/lkml/20220630091231.1456789-3-dylany@fb.com/T/#re5daa4d5b6e4390ecf024315d9693e5d18d61f10
			l.loop.PrepareRecv(l.fd, cb)
		}
	}
	l.loop.PrepareRecv(l.fd, cb)
}

// shutdown tcp (both) then close fd
func (l *TcpConn) shutdown(err error) {
	if err == nil {
		panic("tcp conn missing shutdown reason")
	}
	if l.shutdownError != nil {
		return
	}
	l.shutdownError = err
	l.loop.PrepareShutdown(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
		if !(errno == 0 || errno == syscall.ENOTCONN) {
			slog.Debug("tcp conn shutdown", "fd", l.fd, "errno", errno, "res", res, "flags", flags)
		}
		if errno != 0 {
			l.lsn.remove(l.fd)
			l.up.Closed(l.shutdownError)
			return
		}
		l.loop.PrepareClose(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
			if errno != 0 {
				slog.Debug("tcp conn close", "fd", l.fd, "errno", errno, "res", res, "flags", flags)
			}
			l.lsn.remove(l.fd)
			l.up.Closed(l.shutdownError)
		})
	})
}
