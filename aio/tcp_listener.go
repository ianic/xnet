package aio

import (
	"log/slog"
	"net"
	"syscall"
	"unsafe"

	_ "unsafe"

	"golang.org/x/sys/unix"
)

type TCPListener struct {
	loop        *Loop
	fd          int
	port        int
	accepted    Accepted
	connections map[int]*TCPConn
}

func (l *TCPListener) accept() {
	var cb completionCallback
	cb = func(res int32, flags uint32, err *ErrErrno) {
		if err == nil {
			fd := int(res)
			// create new tcp connection and bind it with upstream layer
			tc := newTcpConn(l.loop, func() { delete(l.connections, fd) }, fd)
			l.accepted(fd, tc)
			l.connections[fd] = tc
			return
		}
		if err.Temporary() {
			l.loop.prepareMultishotAccept(l.fd, cb)
			return
		}
		if !err.Canceled() {
			slog.Debug("listener accept", "fd", l.fd, "errno", err, "res", res, "flags", flags)
		}
	}
	l.loop.prepareMultishotAccept(l.fd, cb)
}

func (l *TCPListener) Close() {
	l.close(true)
}

func (l *TCPListener) close(shutdownConnections bool) {
	l.loop.prepareCancelFd(l.fd, func(res int32, flags uint32, err *ErrErrno) {
		if err != nil {
			slog.Debug("listener cancel", "fd", l.fd, "err", err, "res", res, "flags", flags)
		}
		if shutdownConnections {
			for _, conn := range l.connections {
				conn.shutdown(ErrListenerClose)
			}
		}
		delete(l.loop.listeners, l.fd)
	})
}

func socket(sa syscall.Sockaddr) (int, error) {
	domain := syscall.AF_INET
	switch sa.(type) {
	case *syscall.SockaddrInet6:
		domain = syscall.AF_INET6
	}
	return syscall.Socket(domain, syscall.SOCK_STREAM, 0)
}

func listen(sa syscall.Sockaddr, domain int) (int, int, error) {
	port := 0
	fd, err := syscall.Socket(domain, syscall.SOCK_STREAM, 0)
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

// resolveTCPAddr converts string address to syscall.Scokaddr interface used in
// other syscall calls.
// "www.google.com:80"
// "[::1]:0"
// "127.0.0.1:1234"
func resolveTCPAddr(addr string) (syscall.Sockaddr, int, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, 0, err
	}
	ip := tcpAddr.IP
	port := tcpAddr.Port
	if ip4 := ip.To4(); ip4 != nil {
		return &syscall.SockaddrInet4{Port: port, Addr: [4]byte(ip4)}, syscall.AF_INET, nil
	}
	return &syscall.SockaddrInet6{Port: port, Addr: [16]byte(ip)}, syscall.AF_INET6, nil
}

//go:linkname sockaddr syscall.Sockaddr.sockaddr
func sockaddr(addr syscall.Sockaddr) (unsafe.Pointer, uint32, error)
