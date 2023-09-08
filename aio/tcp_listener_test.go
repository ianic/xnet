package aio

import (
	"syscall"
	"testing"
)

func TestResolveTCPAddr4(t *testing.T) {
	cases := []struct {
		addr string
		ip   [4]byte
		port int
	}{
		{"127.0.0.1:8080", [4]byte{127, 0, 0, 1}, 8080},
		{"4.4.4.4:0", [4]byte{4, 4, 4, 4}, 0},
	}
	for _, c := range cases {
		so, err := resolveTCPAddr(c.addr)
		if err != nil {
			t.Fatal(err)
		}
		so4, ok := so.(*syscall.SockaddrInet4)
		if !ok {
			t.Fatalf("wrong class")
		}
		if so4.Port != c.port || so4.Addr != c.ip {
			t.Fatalf("parse failed")
		}
	}
}

func TestResolveTCPAddr6(t *testing.T) {
	cases := []struct {
		addr string
		ip   [16]byte
		port int
	}{
		{"[2001:0000:130F:0000:0000:09C0:876A:130B]:1234", [16]byte{32, 1, 0, 0, 19, 15, 0, 0, 0, 0, 9, 192, 135, 106, 19, 11}, 1234},
	}
	for _, c := range cases {
		so, err := resolveTCPAddr(c.addr)
		if err != nil {
			t.Fatal(err)
		}
		so6, ok := so.(*syscall.SockaddrInet6)
		if !ok {
			t.Fatalf("wrong class")
		}
		if so6.Port != c.port || so6.Addr != c.ip {
			t.Fatalf("parse failed %d", so6.Addr)
		}
	}
}
