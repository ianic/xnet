package aio

import (
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNetworkConnectWriteStdLib(t *testing.T) {
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, portStr, err := net.SplitHostPort(listen.Addr().String())
	require.NoError(t, err)
	// t.Logf("running test server at port %s", portStr)

	data := []byte("testdata1234567890")
	go func() {
		testSender(t, fmt.Sprintf("127.0.0.1:%s", portStr), data)
	}()

	var readBuffer []byte
	conn, err := listen.Accept()
	require.NoError(t, err)
	chunk := make([]byte, 8)
	for {
		n, err := conn.Read(chunk)
		if n == 0 {
			break
		}
		require.NoError(t, err)
		require.True(t, n >= 0)
		readBuffer = append(readBuffer, chunk[:n]...)
	}
	conn.Close()
	listen.Close()

	require.Equal(t, data, readBuffer)
}

func testSender(t *testing.T, addr string, data []byte) {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	require.Nil(t, err)
	require.NotNil(t, conn)
	n, err := conn.Write(data)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	conn.Close()
}

type testConn struct {
	received [][]byte
	closed   bool
}

func (c *testConn) Received(buf []byte) {
	c.received = append(c.received, toOwn(buf))
}
func (c *testConn) Sent() {}
func (c *testConn) Closed(error) {
	c.closed = true
}

func toOwn(buf []byte) []byte {
	own := make([]byte, len(buf))
	copy(own, buf)
	return own
}

func TestTCPListener(t *testing.T) {
	loop, err := New(Options{
		RingEntries:      16,
		RecvBuffersCount: 8,
		RecvBufferLen:    1024,
	})

	require.NoError(t, err)
	defer loop.Close()

	conn := testConn{}
	// called when tcp listener accepts tcp connection
	tcpAccepted := func(fd int, tc *TcpConn) {
		// t.Logf("accepted fd %d\n", fd)
		tc.Bind(&conn)
	}
	// start listener
	lsn, err := NewTcpListener(loop, "[::1]:0", tcpAccepted)
	require.NoError(t, err)
	// t.Logf("tcp listener started at port %d", lsn.Port())

	data := testRandomBuf(t, 1024*4)
	go func() {
		testSender(t, fmt.Sprintf("[::1]:%d", lsn.Port()), data)
	}()

	// accept connection
	loop.RunOnce()
	lsn.Close(false)
	// run until connection is closed
	loop.RunUntilDone()

	require.True(t, len(conn.received) >= 4)
	testRequireEqualBuffers(t, data, conn.received)

	require.True(t, conn.closed, "conn.Closed should be called")
}

func testRequireEqualBuffers(t *testing.T, expected []byte, actual [][]byte) {
	nn := 0
	for _, buf := range actual {
		require.True(t, len(expected) >= nn+len(buf))
		require.Equal(t, expected[nn:nn+len(buf)], buf)
		nn += len(buf)
	}
	require.Equal(t, len(expected), nn)
}

func testRandomBuf(t *testing.T, size int) []byte {
	f, err := os.Open("/dev/random")
	require.NoError(t, err)
	buf := make([]byte, size)
	_, err = io.ReadFull(f, buf)
	require.NoError(t, err)
	return buf
}
