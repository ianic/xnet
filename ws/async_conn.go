package ws

import (
	"errors"
	"io"
	"log/slog"
)

// lower layer, tcp connection
type TcpConn interface {
	SendBuffers([][]byte)
	Close()
}

// upper layer's events handler interface
type Upstream interface {
	Received([]byte)
	Closed(error)
	Sent()
}

// AsyncConn
// Makes copy of the payload before passing it downstream
type AsyncConn struct {
	tc                TcpConn
	up                Upstream
	permessageDeflate bool       // connection option
	fs                frameState // partial frame parsing state
	partialFrame      *Frame
}

type frameState struct {
	pending  []byte // unprocessed part of the received buffer
	recvMore int    // how much bytes is needed for frame parsing to advance
}

func (fs *frameState) received(buf []byte) []byte {
	if len(buf) < fs.recvMore {
		fs.pending = append(fs.pending, buf...)
		fs.recvMore -= len(buf)
		return nil // nothing to process waiting for more
	}
	if len(fs.pending) > 0 {
		fs.recvMore = 0
		return append(fs.pending, buf...) // process pending + buf
	}
	return buf // nothing pending process buf
}

func (fs *frameState) unprocessed(buf []byte, recvMore int) {
	fs.pending = append(fs.pending, buf...)
	fs.recvMore = recvMore
}

func (fs *frameState) reset() {
	fs.pending = nil
	fs.recvMore = 0
}

func (c *AsyncConn) Received(buf []byte) {
	if b := c.fs.received(buf); b != nil {
		if err := c.readFrames(b); err != nil {
			slog.Debug("read frame failed", slog.String("error", err.Error()))
			c.tc.Close()
		}
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
				// everything in buffer processed
				return nil
			}
			var erm *ErrReadMore
			if errors.As(err, &erm) {
				c.fs.unprocessed(bbr.pending(), erm.Bytes)
				return nil
			}
			return err
		}
		c.fs.reset()
		if frame.isControl() {
			c.handleControl(frame)
			continue
		}

		var full *Frame = nil
		full, c.partialFrame, err = c.partialFrame.defragment(&frame)
		if err != nil {
			return err
		}
		if full != nil {
			_, payload, err := toMessage(full, c.permessageDeflate)
			if err != nil {
				return err
			}
			c.up.Received(payload) // send message downstream
		}
	}
}

func (c *AsyncConn) handleControl(frame Frame) {
	switch frame.opcode {
	case Ping:
		c.send(Pong, toOwnCopy(frame.payload))
	case Pong:
		// nothing to do on pong
		return
	case Close:
		c.send(Close, toOwnCopy(frame.payload))
	default:
		panic("not a control frame")
	}
}

func toOwnCopy(payload []byte) []byte {
	if len(payload) == 0 {
		return nil
	}
	dst := make([]byte, len(payload))
	copy(dst, payload)
	return dst
}

func (c *AsyncConn) send(opcode OpCode, payload []byte) error {
	buffers, err := encodeFrame(opcode, payload, c.permessageDeflate)
	if err != nil {
		return err
	}
	c.tc.SendBuffers(buffers)
	return nil
}

func (c *AsyncConn) Send(payload []byte) {
	if err := c.send(Binary, payload); err != nil {
		c.Close()
	}
}

func (c *AsyncConn) Close() {
	c.tc.Close()
}

func (c *AsyncConn) Closed(err error) {
	c.up.Closed(err)
}

func (c *AsyncConn) Sent() {
	c.up.Sent()
}

func (c *AsyncConn) Bind(up Upstream) {
	c.up = up
}
