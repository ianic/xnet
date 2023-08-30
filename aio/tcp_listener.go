package aio

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

type Binder func(fd int, tcpConn *TcpConn, bind func(Upstream))

type TcpListener struct {
	loop  *Loop
	fd    int
	port  int
	bind  Binder
	conns map[int]*TcpConn
}

// NewTcpListener
// ipPort examples:
// ip4:  "127.0.0.1:8080",
// ip16: "[::1]:80"
//
// Binder function will be called when new tcp connection is accepted. That
// connects newly created connection and upstream handler. It's up to the
// upstream handler to call bind when ready. On in any other time when it need
// to change upstream layer. For example during after websocket handshake layer
// can be change from one which were handling handshake to one which will handle
// websocket frames.
func NewTcpListener(loop *Loop, ipPort string, bind Binder) (*TcpListener, error) {
	so, err := ParseIPPort(ipPort)
	if err != nil {
		return nil, err
	}
	fd, port, err := listen(so)
	if err != nil {
		return nil, err
	}
	l := &TcpListener{
		fd:    fd,
		port:  port,
		loop:  loop,
		bind:  bind,
		conns: make(map[int]*TcpConn),
	}
	l.accept()
	return l, nil
}

func (l *TcpListener) accept() {
	l.loop.PrepareMultishotAccept(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
		if errno == 0 {
			fd := int(res)
			// create new tcp connection and bind it with upstream layer
			tc := newTcpConn(l.loop, l, fd)
			l.bind(fd, tc, tc.Bind)
			l.conns[fd] = tc
			return
		}
		if errno != syscall.ECANCELED {
			slog.Debug("listener accept", "fd", l.fd, "errno", errno, "res", res, "flags", flags)
		}
	})
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

func (l *TcpListener) ConnCount() int { return len(l.conns) }
func (l *TcpListener) Port() int      { return l.port }

func (l *TcpListener) remove(fd int) {
	delete(l.conns, fd)
}

func listen(sa syscall.Sockaddr) (int, int, error) {
	port := 0
	switch v := sa.(type) {
	case *syscall.SockaddrInet4:
		port = v.Port
	case *syscall.SockaddrInet6:
		port = v.Port
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return 0, 0, err
	}
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		return 0, 0, err
	}
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
		return 0, 0, err
	}
	if err := syscall.Bind(fd, sa); err != nil {
		return 0, 0, err
	}
	if port == 0 {
		// get system assigned port
		if sn, err := syscall.Getsockname(fd); err == nil {
			switch v := sn.(type) {
			case *syscall.SockaddrInet4:
				port = v.Port
			case *syscall.SockaddrInet6:
				port = v.Port
			}
		}
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		return 0, 0, err
	}
	if err := syscall.Listen(fd, 128); err != nil {
		return 0, 0, err
	}
	return fd, port, nil
}

func ParseIPPort(ipPort string) (syscall.Sockaddr, error) {
	ipStr, portStr, err := net.SplitHostPort(ipPort)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("net.ParseIP failed on %s", ipStr)
	}
	if ip4 := ip.To4(); ip4 != nil {
		return &syscall.SockaddrInet4{Port: port, Addr: [4]byte(ip4)}, nil
	}
	return &syscall.SockaddrInet6{Port: port, Addr: [16]byte(ip)}, nil
}
