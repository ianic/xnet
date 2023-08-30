package aio

import (
	"errors"
	"io"
	"log/slog"
	"syscall"
)

var (
	ErrListenerClose = errors.New("listener closed connection")
	ErrUpstreamClose = errors.New("upstream closed connection")
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

func newTcpConn(loop *Loop, lsn *TcpListener, fd int) *TcpConn {
	return &TcpConn{loop: loop, fd: fd, lsn: lsn}
}

func (tc *TcpConn) Bind(up Upstream) {
	startRecv := tc.up == nil
	tc.up = up
	if startRecv {
		tc.recvLoop()
	}
}

// TODO: add correlation id (userdata) for send/sent connecting
func (tc *TcpConn) Send(data []byte) {
	nn := 0 // number of bytes sent
	var cb completionCallback
	cb = func(res int32, flags uint32, errno syscall.Errno) {
		nn += int(res) // bytes written so far
		if errno > 0 {
			tc.shutdown(errno)
			return
		}
		if nn >= len(data) {
			// all sent call callback
			tc.up.Sent()
			return
		}
		// send rest of the data
		tc.loop.PrepareSend(tc.fd, data[nn:], cb)
		// new send prepared
	}
	tc.loop.PrepareSend(tc.fd, data, cb)
}

func (tc *TcpConn) SendBuffers(buffers [][]byte) {
	var cb completionCallback
	cb = func(res int32, flags uint32, errno syscall.Errno) {
		n := int(res)
		if errno > 0 {
			tc.shutdown(errno)
			return
		}
		consume(&buffers, n)
		if len(buffers) == 0 {
			tc.up.Sent()
			return
		}
		// send rest of the data
		tc.loop.PrepareWritev(tc.fd, buffers, cb)
		// new send prepared
	}
	tc.loop.PrepareWritev(tc.fd, buffers, cb)
}

// consume removes data from a slice of byte slices, for writev.
// copied from:
// https://github.com/golang/go/blob/140266fe7521bf75bf0037f12265190213cc8e7d/src/internal/poll/fd.go#L69
func consume(v *[][]byte, n int) {
	for len(*v) > 0 {
		ln0 := len((*v)[0])
		if ln0 > n {
			(*v)[0] = (*v)[0][n:]
			return
		}
		n -= ln0
		(*v)[0] = nil
		*v = (*v)[1:]
	}
}

func (tc *TcpConn) Close() {
	tc.shutdown(ErrUpstreamClose)
}

// Allows upper layer to change connection handler.
// Useful in websocket handshake for example.
func (tc *TcpConn) SetUpstream(conn Upstream) {
	tc.up = conn
}

// recvLoop starts multishot recv on fd
// Will receive on fd until error occurs.
func (tc *TcpConn) recvLoop() {
	var cb completionCallback
	cb = func(res int32, flags uint32, errno syscall.Errno) {
		if errno > 0 {
			if TemporaryErrno(errno) {
				slog.Debug("tcp conn read temporary error", "error", errno.Error(), "errno", uint(errno), "flags", flags)
				tc.loop.PrepareRecv(tc.fd, cb)
				return
			}
			if errno != syscall.ECONNRESET {
				slog.Warn("tcp conn read error", "error", errno.Error(), "errno", uint(errno), "flags", flags)
			}
			tc.shutdown(errno)
			return
		}
		if res == 0 {
			tc.shutdown(io.EOF)
			return
		}
		buf, id := tc.loop.buffers.get(res, flags)
		tc.up.Received(buf)
		tc.loop.buffers.release(buf, id)
		if !isMultiShot(flags) {
			slog.Debug("tcp conn multishot terminated", slog.Uint64("flags", uint64(flags)), slog.Uint64("errno", uint64(errno)))
			// io_uring can terminate multishot recv when cqe is full
			// need to restart it then
			// ref: https://lore.kernel.org/lkml/20220630091231.1456789-3-dylany@fb.com/T/#re5daa4d5b6e4390ecf024315d9693e5d18d61f10
			tc.loop.PrepareRecv(tc.fd, cb)
		}
	}
	tc.loop.PrepareRecv(tc.fd, cb)
}

// shutdown tcp (both) then close fd
func (tc *TcpConn) shutdown(err error) {
	if err == nil {
		panic("tcp conn missing shutdown reason")
	}
	if tc.shutdownError != nil {
		return
	}
	tc.shutdownError = err
	tc.loop.PrepareShutdown(tc.fd, func(res int32, flags uint32, errno syscall.Errno) {
		if !(errno == 0 || errno == syscall.ENOTCONN) {
			slog.Debug("tcp conn shutdown", "fd", tc.fd, "errno", errno, "res", res, "flags", flags)
		}
		if errno != 0 {
			tc.lsn.remove(tc.fd)
			tc.up.Closed(tc.shutdownError)
			return
		}
		tc.loop.PrepareClose(tc.fd, func(res int32, flags uint32, errno syscall.Errno) {
			if errno != 0 {
				slog.Debug("tcp conn close", "fd", tc.fd, "errno", errno, "res", res, "flags", flags)
			}
			tc.lsn.remove(tc.fd)
			tc.up.Closed(tc.shutdownError)
		})
	})
}
