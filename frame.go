package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf8"
)

type OpCode byte

const (
	Continuation OpCode = 0
	Text         OpCode = 1
	Binary       OpCode = 2
	Close        OpCode = 8
	Ping         OpCode = 9
	Pong         OpCode = 0xa
)

const (
	defaultCloseCode = 1000
)

var (
	ErrorReservedOpcode = errors.New("reserved opcode")
	ErrorInvalidFrame   = errors.New("invalid frame")

	ErrTooBigPayloadForControlFrame = errors.New("too big payload for control frame")
	ErrInvalidCloseCode             = errors.New("invalid close code")
	ErrFragmentedControlFrame       = errors.New("fragmented control frame")
	ErrInvalidUtf8Payload           = errors.New("invalid utf8 payload")
)

func (o OpCode) verify() error {
	if o <= Binary || (o >= Close && o <= Pong) {
		return nil
	}
	return ErrorReservedOpcode

}

const (
	finMask    byte = 0b1000_0000
	rsv1Mask   byte = 0b0100_0000
	rsv2Mask   byte = 0b0010_0000
	rsv3Mask   byte = 0b0001_0000
	opcodeMask byte = 0b0000_1111
	maskMask   byte = 0b1000_0000
	lenMask    byte = 0b0111_1111
)

type Frame struct {
	flags   byte
	opcode  OpCode
	payload []byte
}

func (f Frame) Fin() bool {
	return f.flags&finMask != 0
}

func (f Frame) Rsv1() bool {
	return f.flags&rsv1Mask != 0
}

func (f Frame) Rsv2() bool {
	return f.flags&rsv2Mask != 0
}

func (f Frame) Rsv3() bool {
	return f.flags&rsv3Mask != 0
}

func (f Frame) closeCode() uint16 {
	if f.opcode != Close {
		return 0
	}
	if len(f.payload) == 1 { //invalid
		return 0
	}
	if len(f.payload) == 0 {
		return defaultCloseCode
	}
	return binary.BigEndian.Uint16(f.payload[0:2])
}

func (f Frame) closePayload() []byte {
	if len(f.payload) > 2 {
		return f.payload[2:]
	}
	return nil
}

func (f Frame) verify() error {
	if err := f.opcode.verify(); err != nil {
		return err
	}
	if !f.isControl() {
		if f.opcode == Text {
			return f.verifyText()
		}
		return nil
	}
	if err := f.verifyControl(); err != nil {
		return err
	}
	if f.opcode == Close {
		return f.verifyClose()
	}
	return nil
}

// Verify single frame (no fragmentation) utf8 text. If fragmented we need
// to concatenate payload from all fragments can't do that from single
// frame.
func (f Frame) verifyText() error {
	if f.Fin() && !utf8.Valid(f.payload) {
		return ErrInvalidUtf8Payload
	}
	return nil
}

func (f Frame) verifyClose() error {
	if !utf8.Valid(f.closePayload()) {
		return ErrInvalidUtf8Payload
	}
	return f.verifyCloseCode()
}
func (f Frame) verifyCloseCode() error {
	cc := f.closeCode()
	if (cc >= 1000 && cc <= 1003) ||
		(cc >= 1007 && cc <= 1011) ||
		(cc >= 3000 && cc <= 4999) {
		return nil
	}
	return ErrInvalidCloseCode
}

func (f Frame) verifyControl() error {
	if len(f.payload) > 125 {
		return ErrTooBigPayloadForControlFrame
	}
	if !f.Fin() {
		return ErrFragmentedControlFrame
	}
	return nil
}

func (f Frame) isControl() bool {
	return f.opcode == Close ||
		f.opcode == Ping ||
		f.opcode == Pong
}

// NewFrameFromReader uses buffered reader to decode frame. Blocks if there is
// not enough data in reader.
// Returns io.EOF when there is no frame in rdr. When last frame finishes on reader buffer boundary.
// Returns io.ErrUnexpectedEOF if frame parsing starts but then gets out of bytes.
//
// Note: ReadByte returns io.EOF when buffer emtpy, ReadFull returns ErrUnexpectedEOF!
func NewFrameFromReader(rdr *bufio.Reader) (*Frame, error) {
	first, err := rdr.ReadByte()
	if err != nil {
		return nil, err
	}
	second, err := rdr.ReadByte()
	if err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	// decode first two bytes
	flags := first & ^opcodeMask
	opcode := OpCode(first & opcodeMask)
	masked := second&maskMask != 0
	payloadLen := uint64(second &^ maskMask)

	// decode payload len, read more bytes if needed
	switch payloadLen {
	case 126:
		buf := make([]byte, 2)
		if _, err := io.ReadFull(rdr, buf); err != nil {
			return nil, err
		}
		payloadLen = uint64(binary.BigEndian.Uint16(buf))
	case 127:
		buf := make([]byte, 8)
		if _, err := io.ReadFull(rdr, buf); err != nil {
			return nil, err
		}
		payloadLen = binary.BigEndian.Uint64(buf)
	}

	frame := Frame{flags: flags, opcode: opcode}

	var mask []byte
	if masked {
		mask = make([]byte, 4)
		if _, err := io.ReadFull(rdr, mask); err != nil {
			return nil, err
		}
	}

	// read payload
	if payloadLen > 0 {
		frame.payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(rdr, frame.payload); err != nil {
			return nil, err
		}

		if masked {
			maskUnmask(mask, frame.payload)
		}
	}

	if err := frame.verify(); err != nil {
		return nil, err
	}

	return &frame, nil
}

func NewFrame(buf []byte) (*Frame, error) {
	rdr := bufio.NewReader(bytes.NewReader(buf))
	return NewFrameFromReader(rdr)
}

func maskUnmask(mask []byte, buf []byte) {
	for i, c := range buf {
		buf[i] = c ^ mask[i%4]
	}
}

type Iterator struct {
	rdr *bufio.Reader
	err error
}

func (i *Iterator) Next() *Frame {
	frame, err := NewFrameFromReader(i.rdr)
	if err != io.EOF {
		i.err = err
	}
	return frame
}

func NewIterator(rdr *bufio.Reader) *Iterator {
	return &Iterator{rdr: rdr, err: nil}
}
