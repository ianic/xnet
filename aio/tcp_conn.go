package aio

import (
	"log/slog"
	"syscall"
)

// upper layer events handler interface, to be called by me
type Conn interface {
	Received([]byte)
	Closed(error)
	Sent(error)
}

type TcpConn struct {
	lsn  *TcpListener
	loop *Loop
	fd   int
	conn Conn
}

// TODO: add correlation id (userdata) for send/sent connecting
func (l *TcpConn) Send(data []byte) error {
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

func (l *TcpConn) Close() {
	l.shutdown(nil)
}

// Allows upper layer to change connection handler.
// Useful in websocket handshake for example.
func (l *TcpConn) SetConn(conn Conn) {
	l.conn = conn
}

// recvLoop starts multishot recv on fd
// Will receive on fd until error occurs.
func (l *TcpConn) recvLoop() error {
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
		l.conn.Received(buf)
		l.loop.buffers.release(buf, id)
		if !isMultiShot(flags) {
			slog.Debug("multishot terminated", slog.Uint64("flags", uint64(flags)), slog.Uint64("errno", uint64(errno)))
			// io_uring can terminate multishot recv when cqe is full
			// need to restart it then
			// ref: https://lore.kernel.org/lkml/20220630091231.1456789-3-dylany@fb.com/T/#re5daa4d5b6e4390ecf024315d9693e5d18d61f10
			l.loop.PrepareRecv(l.fd, cb)
		}
	}
	return l.loop.PrepareRecv(l.fd, cb)
}

// shutdown tcp (both) then close fd
func (l *TcpConn) shutdown(err error) {
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
