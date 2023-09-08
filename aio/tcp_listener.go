package aio

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"syscall"

	_ "unsafe"

	"golang.org/x/sys/unix"
)

type TcpListener struct {
	loop        *Loop
	fd          int
	port        int
	accepted    Accepted
	connections map[int]*TcpConn
}

func (l *TcpListener) accept() {
	l.loop.PrepareMultishotAccept(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
		if errno == 0 {
			fd := int(res)
			// create new tcp connection and bind it with upstream layer
			tc := newTcpConn(l.loop, func() { delete(l.connections, fd) }, fd)
			l.accepted(fd, tc)
			l.connections[fd] = tc
			return
		}
		if errno != syscall.ECANCELED {
			slog.Debug("listener accept", "fd", l.fd, "errno", errno, "res", res, "flags", flags)
		}
	})
}

func (l *TcpListener) Close() {
	l.close(true)
}

func (l *TcpListener) close(shutdownConnections bool) {
	l.loop.PrepareCancelFd(l.fd, func(res int32, flags uint32, errno syscall.Errno) {
		if errno != 0 {
			slog.Debug("listener cancel", "fd", l.fd, "errno", errno, "res", res, "flags", flags)
		}
		if shutdownConnections {
			for _, conn := range l.connections {
				conn.shutdown(ErrListenerClose)
			}
		}
		delete(l.loop.listeners, l.fd)
	})
}

func (l *TcpListener) ConnCount() int { return len(l.connections) }
func (l *TcpListener) Port() int      { return l.port }

func socket(sa syscall.Sockaddr) (int, error) {
	domain := syscall.AF_INET
	switch sa.(type) {
	case *syscall.SockaddrInet6:
		domain = syscall.AF_INET6
	}
	return syscall.Socket(domain, syscall.SOCK_STREAM, 0)
}

func listen(sa syscall.Sockaddr) (int, int, error) {
	port := 0
	domain := syscall.AF_INET
	switch v := sa.(type) {
	case *syscall.SockaddrInet4:
		port = v.Port
	case *syscall.SockaddrInet6:
		port = v.Port
		domain = syscall.AF_INET6
	}
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

// "www.google.com:80"
func ResolveTCPAddr(addr string) (syscall.Sockaddr, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}
	ip := tcpAddr.IP
	port := tcpAddr.Port
	if ip4 := ip.To4(); ip4 != nil {
		return &syscall.SockaddrInet4{Port: port, Addr: [4]byte(ip4)}, nil
	}
	return &syscall.SockaddrInet6{Port: port, Addr: [16]byte(ip)}, nil

}
