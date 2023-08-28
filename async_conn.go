package ws

import (
	"errors"
	"io"
)

// upstream stdin
type Stream interface {
	Send([]byte) error
	Close(error)
}

// downstream stdout
type Handler interface {
	Received([]byte)
	Disconnected(error)
}

type AsyncConn struct {
	stream  Stream
	handler Handler
	// connection options
	permessageDeflate bool
	// fragmented message state
	payload           []byte
	opcode            OpCode
	prevFrameFragment Fragment
	compressed        bool
	// partial frame parsing state
	pending  []byte
	recvMore int
}

func (c *AsyncConn) resetFrameParsingState() {
	c.pending = nil
	c.recvMore = 0
}

func (c *AsyncConn) resetFragmentedMessageState() {
	c.payload = nil
	c.opcode = None
	c.prevFrameFragment = fragSingle
	c.compressed = false
}

func (c *AsyncConn) Received(buf []byte) {
	if len(buf) < c.recvMore {
		c.pending = append(c.pending, buf...)
		c.recvMore -= len(buf)
		return
	}

	bbuf := buf
	if len(c.pending) > 0 {
		bbuf = append(c.pending, buf...)
	}
	// TODO informacija korisiti li vlatiti kopiju buffera ili je shared s nivoom prije
	// ili jednostavno od ove tocke dalje osiguraj da se vrati buffer u io_uring
	// da drugi vise ne moraju misliti o tome, da ne propagiramo tu informaciju dalje
	if err := c.readFrames(bbuf); err != nil {
		c.stream.Close(err)
	}
}

func (c *AsyncConn) readFrames(buf []byte) error {
	bbr := &bufferBytesReader{buf: buf}
	rdr := FrameReader{rd: bbr}

	for {
		frame, err := rdr.Read()
		if err != nil {
			if err == io.EOF {
				// reached end of the buffer cleanly
				// everything in buffer consumed
				return nil
			}
			var erm *ErrReadMore
			if errors.As(err, &erm) {
				c.recvMore = erm.Bytes
				if p := bbr.pending(); len(p) > 0 {
					c.pending = append(c.pending, p...)
				}
				return nil
			}
			return err
		}
		c.resetFrameParsingState()
		if frame.isControl() {
			c.handleControl(frame)
			continue
		}
		if err := verifyFrame(frame, c.prevFrameFragment, c.permessageDeflate); err != nil {
			return err
		}

		if frame.first() {
			c.compressed = frame.rsv1()
			c.opcode = frame.opcode
			c.payload = frame.payload
		} else {
			c.payload = append(c.payload, frame.payload...)
		}

		if frame.fin {
			if c.compressed {
				c.payload, err = Decompress(c.payload)
				if err != nil {
					return err
				}
			}
			if err := verifyMessage(c.opcode, c.payload); err != nil {
				return err
			}
			c.handler.Received(c.payload)
			c.resetFragmentedMessageState()
			continue
		}
		c.prevFrameFragment = frame.fragment()
	}
}

func (c *AsyncConn) handleControl(frame Frame) {
	switch frame.opcode {
	case Ping:
		c.send(Pong, frame.payload)
	case Pong:
		// nothing to do on pong
		return
	case Close:
		c.send(Close, frame.payload)
	default:
		panic("not a control frame")
	}
}

func (c *AsyncConn) send(opcode OpCode, payload []byte) {
	buffers, err := encodeFrame(opcode, payload, c.permessageDeflate)
	if err != nil {
		// TODO
		return
	}
	nn := 0
	for _, b := range buffers {
		nn += len(b)
	}
	buf := make([]byte, nn)
	nn = 0
	for _, b := range buffers {
		copy(buf[nn:], b)
		nn += len(b)
	}
	c.stream.Send(buf)
}

func (c *AsyncConn) Disconnected(err error) {

}
