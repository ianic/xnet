package aio

import (
	"errors"
	"io"
	"log/slog"
	"runtime"
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

type TCPConn struct {
	closedCallback func()
	loop           *Loop
	fd             int
	up             Upstream
	shutdownError  error
}

func newTcpConn(loop *Loop, closedCallback func(), fd int) *TCPConn {
	return &TCPConn{loop: loop, fd: fd, closedCallback: closedCallback}
}

// Bind connects this connection and upstream handler. It's up to the
// upstream handler to call bind when ready. On in any other time when it need
// to change upstream layer. For example during after websocket handshake layer
// can be change from one which were handling handshake to one which will handle
// websocket frames.
func (tc *TCPConn) Bind(up Upstream) {
	startRecv := tc.up == nil
	tc.up = up
	if startRecv {
		tc.recvLoop()
	}
}

// TODO: add correlation id (userdata) for send/sent connecting
func (tc *TCPConn) Send(data []byte) {
	nn := 0 // number of bytes sent
	var cb completionCallback
	var pinner runtime.Pinner
	pinner.Pin(&data[0])
	cb = func(res int32, flags uint32, err *ErrErrno) {
		nn += int(res) // bytes written so far
		if err != nil {
			pinner.Unpin()
			tc.shutdown(err)
			return
		}
		if nn >= len(data) {
			pinner.Unpin()
			tc.up.Sent() // all sent call callback
			return
		}
		// send rest of the data
		tc.loop.prepareSend(tc.fd, data[nn:], cb)
	}
	tc.loop.prepareSend(tc.fd, data, cb)
}

func (tc *TCPConn) SendBuffers(buffers [][]byte) {
	var cb completionCallback
	var pinner runtime.Pinner
	for _, buf := range buffers {
		pinner.Pin(&buf[0])
	}
	cb = func(res int32, flags uint32, err *ErrErrno) {
		n := int(res)
		if err != nil {
			pinner.Unpin()
			tc.shutdown(err)
			return
		}
		consumeBuffers(&buffers, n)
		if len(buffers) == 0 {
			pinner.Unpin()
			tc.up.Sent()
			return
		}
		// send rest of the data
		iovecs := buffersToIovec(buffers)
		pinner.Pin(&iovecs[0])
		tc.loop.prepareWritev(tc.fd, iovecs, cb)
	}
	iovecs := buffersToIovec(buffers)
	pinner.Pin(&iovecs[0])
	tc.loop.prepareWritev(tc.fd, iovecs, cb)
}

func buffersToIovec(buffers [][]byte) []syscall.Iovec {
	var iovecs []syscall.Iovec
	for _, buf := range buffers {
		if len(buf) == 0 {
			continue
		}
		iovecs = append(iovecs, syscall.Iovec{Base: &buf[0]})
		iovecs[len(iovecs)-1].SetLen(len(buf))
	}
	return iovecs
}

// consumeBuffers removes data from a slice of byte slices, for writev.
// copied from:
// https://github.com/golang/go/blob/140266fe7521bf75bf0037f12265190213cc8e7d/src/internal/poll/fd.go#L69
func consumeBuffers(v *[][]byte, n int) {
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

func (tc *TCPConn) Close() {
	tc.shutdown(ErrUpstreamClose)
}

// recvLoop starts multishot recv on fd
// Will receive on fd until error occurs.
func (tc *TCPConn) recvLoop() {
	var cb completionCallback
	cb = func(res int32, flags uint32, err *ErrErrno) {
		if err != nil {
			if err.Temporary() {
				slog.Debug("tcp conn read temporary error", "error", err.Error())
				tc.loop.prepareRecv(tc.fd, cb)
				return
			}
			if !err.ConnectionReset() {
				slog.Warn("tcp conn read error", "error", err.Error())
			}
			tc.shutdown(err)
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
			slog.Debug("tcp conn multishot terminated", slog.Uint64("flags", uint64(flags)), slog.String("error", err.Error()))
			// io_uring can terminate multishot recv when cqe is full
			// need to restart it then
			// ref: https://lore.kernel.org/lkml/20220630091231.1456789-3-dylany@fb.com/T/#re5daa4d5b6e4390ecf024315d9693e5d18d61f10
			tc.loop.prepareRecv(tc.fd, cb)
		}
	}
	tc.loop.prepareRecv(tc.fd, cb)
}

// shutdown tcp (both) then close fd
func (tc *TCPConn) shutdown(err error) {
	if err == nil {
		panic("tcp conn missing shutdown reason")
	}
	if tc.shutdownError != nil {
		return
	}
	tc.shutdownError = err
	tc.loop.prepareShutdown(tc.fd, func(res int32, flags uint32, err *ErrErrno) {
		if err != nil {
			if !err.ConnectionReset() {
				slog.Debug("tcp conn shutdown", "fd", tc.fd, "err", err, "res", res, "flags", flags)
			}
			if tc.closedCallback != nil {
				tc.closedCallback()
			}
			tc.up.Closed(tc.shutdownError)
			return
		}
		tc.loop.prepareClose(tc.fd, func(res int32, flags uint32, err *ErrErrno) {
			if err != nil {
				slog.Debug("tcp conn close", "fd", tc.fd, "errno", err, "res", res, "flags", flags)
			}
			if tc.closedCallback != nil {
				tc.closedCallback()
			}
			tc.up.Closed(tc.shutdownError)
		})
	})
}
