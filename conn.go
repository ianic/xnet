package ws

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os"
	"time"
	"unicode/utf8"
)

const (
	writeTimeout = 15 * time.Second
	readTimeout  = 60 * time.Second
)

// WebSocket connection
type Conn struct {
	nc                net.Conn // underlying network connection
	fr                FrameReader
	permessageDeflate bool
}

func NewConnection(nc net.Conn, br *bufio.Reader, permessageDeflate bool) Conn {
	return Conn{
		nc:                nc,
		fr:                FrameReader{rd: newBufioBytesReader(br)},
		permessageDeflate: permessageDeflate,
	}
}

// deadlineReader is a wrapper around net.Conn that sets read deadline before
// every Read() call.
type deadlineReader struct {
	nc net.Conn
}

func (d deadlineReader) Read(p []byte) (int, error) {
	if err := d.nc.SetReadDeadline(fromTimeout(readTimeout)); err != nil {
		return 0, err
	}
	return d.nc.Read(p)
}

// Read reads message from underlying net.Conn.
// `opcode` can be Text or Binary, safe to ignore if you know your payload type.
// If opcode is Text payload is then valid utf8 string.
// Handles control frames. Responds on ping with pong. Responds on close. If
// there is nothing to read for more than readTimeout sends ping frame. Sends
// ping even if the connection was write active in that period (can be more
// intelligent).
// FrameReader uses internally bufio.Reader with default configuration. That
// uses [4096] bytes buffer for each connection (not nice constant allocation).
// bufio.Reader will pass [bigger reads] directly to the underlying reader (nice
// optimization).
//
// [4096]: https://github.com/golang/go/blob/99b80993f607e1c6e2f4c14445de103ba6856cfc/src/bufio/bufio.go#L19
// [bigger reads]: https://github.com/golang/go/blob/master/src/bufio/bufio.go#L228
func (c *Conn) Read() (OpCode, []byte, error) {
	opcode, payload, err := c.read()
	if err != nil {
		_ = c.nc.Close()
	}
	return opcode, payload, err
}

func (c *Conn) read() (OpCode, []byte, error) {
	var payload []byte
	opcode := None
	prevFrameFragment := fragSingle
	compressed := false

	for {
		frame, err := c.readFrame()
		if err != nil {
			return None, nil, err
		}
		if frame.isControl() {
			if err := c.handleControl(frame); err != nil {
				return None, nil, err
			}
			continue
		}
		if err := verifyFrame(frame, prevFrameFragment, c.permessageDeflate); err != nil {
			return None, nil, err
		}

		if frame.first() {
			compressed = frame.rsv1()
			opcode = frame.opcode
			payload = frame.payload
		} else {
			payload = append(payload, frame.payload...)
		}

		if frame.fin {
			if compressed {
				payload, err = Decompress(payload)
				if err != nil {
					return None, nil, err
				}
			}
			if err := verifyMessage(opcode, payload); err != nil {
				return None, nil, err
			}
			return opcode, payload, nil
		}

		prevFrameFragment = frame.fragment()
	}
}

func verifyFrame(frame Frame, prevFragment Fragment, permessageDeflate bool) error {
	if err := frame.verifyContinuation(prevFragment); err != nil {
		return err
	}
	if err := frame.verifyRsvBits(permessageDeflate); err != nil {
		return err
	}
	return nil
}

// verify that text message has valid utf8 payload
func verifyMessage(opcode OpCode, payload []byte) error {
	if opcode == Text && !utf8.Valid(payload) {
		return ErrInvalidUtf8Payload
	}
	return nil
}

func (c *Conn) readFrame() (Frame, error) {
	for {
		frame, err := c.fr.Read()
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				if err := c.onReadDeadlineExceeded(); err != nil {
					return Frame{}, err
				}
				continue
			}
			if errors.Is(err, io.EOF) {
				return Frame{}, io.EOF
			}
			return Frame{}, err
		}
		return frame, nil
	}
}

func (c *Conn) onReadDeadlineExceeded() error {
	return c.Write(Ping, nil)
}

func (c *Conn) handleControl(frame Frame) error {
	switch frame.opcode {
	case Ping:
		return c.Write(Pong, frame.payload)
	case Pong:
		// nothing to do on pong
		return nil
	case Close:
		_ = c.Write(Close, frame.payload)
		return io.EOF
	default:
		panic("not a control frame")
	}
}

// WriteBinary sends WebSocket binary frame.
func (c *Conn) WriteBinary(payload []byte) error {
	return c.Write(Binary, payload)
}

// WriteBinary sends WebSocket text frame.
func (c *Conn) WriteText(payload []byte) error {
	return c.Write(Text, payload)
}

// Write prepares message frame, compresses payload if deflate is enabled and
// writes that frame to the underlying net.Conn.
func (c *Conn) Write(opcode OpCode, payload []byte) error {
	buffers, err := encodeFrame(opcode, payload, c.permessageDeflate)
	if err != nil {
		return err
	}
	return c.write(buffers)
}

func encodeFrame(opcode OpCode, payload []byte, permessageDeflate bool) (net.Buffers, error) {
	frame := Frame{fin: true, opcode: opcode, payload: payload}
	if (opcode == Text || opcode == Binary) && permessageDeflate {
		payload, err := Compress(payload)
		if err != nil {
			return nil, err
		}
		frame.deflated = true
		frame.payload = payload
	}
	return frame.encode(), nil
}

// buffers.Write on [unix] systems will call [Writev]. Writev locks at enter so
// it is safe to call it from multiple goroutines.
// If we have concurrent calls to this write they will lock after setting
// deadline, so deadline includes both wait for previous writes to finish and my
// write time.
//
// [unix]: https://github.com/golang/go/blob/99b80993f607e1c6e2f4c14445de103ba6856cfc/src/go/build/syslist.go#L39
// [Writev]: https://github.com/golang/go/blob/2fcfdb96860855be0c88e10e3fd5bb858420cfe2/src/internal/poll/writev.go#L16C10-L16C10
func (c *Conn) write(buffers net.Buffers) error {
	c.nc.SetWriteDeadline(fromTimeout(writeTimeout))
	_, err := buffers.WriteTo(c.nc)
	if err != nil {
		_ = c.nc.Close()
	}
	return err
}

func (c *Conn) Close() error {
	return c.nc.Close()
}

var resetDeadline = time.Time{}

func fromTimeout(dur time.Duration) time.Time {
	return time.Now().Add(dur)
}

func (c *Conn) Fd() uintptr {
	file, err := c.nc.(*net.TCPConn).File()
	if err != nil {
		return 0
	}
	return file.Fd()
}
