// WebSocket Protocol Frame
// reference: https://www.rfc-editor.org/rfc/rfc6455#section-5
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-------+-+-------------+-------------------------------+
//	|F|R|R|R| opcode|M| Payload len |    Extended payload length    |
//	|I|S|S|S|  (4)  |A|     (7)     |             (16/64)           |
//	|N|V|V|V|       |S|             |   (if payload len==126/127)   |
//	| |1|2|3|       |K|             |                               |
//	+-+-+-+-+-------+-+-------------+ - - - - - - - - - - - - - - - +
//	|     Extended payload length continued, if payload len == 127  |
//	+ - - - - - - - - - - - - - - - +-------------------------------+
//	|                               |Masking-key, if MASK set to 1  |
//	+-------------------------------+-------------------------------+
//	| Masking-key (continued)       |          Payload Data         |
//	+-------------------------------- - - - - - - - - - - - - - - - +
//	:                     Payload Data continued ...                :
//	+ - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - +
//	|                     Payload Data continued ...                |
//	+---------------------------------------------------------------+
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"unicode/utf8"
)

var (
	ErrorReservedOpcode             = errors.New("reserved opcode")
	ErrTooBigPayloadForControlFrame = errors.New("too big payload for control frame")
	ErrInvalidCloseCode             = errors.New("invalid close code")
	ErrFragmentedControlFrame       = errors.New("fragmented control frame")
	ErrInvalidUtf8Payload           = errors.New("invalid utf8 payload")
	ErrReservedRsv                  = errors.New("reserved rsv bit is set")
	ErrDeflateNotSupported          = errors.New("rsv1 set but deflate is not supported")
	ErrInvalidFragmentation         = errors.New("invalid frames fragmentation")
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

type Fragment byte

const (
	fragmentUnfragmented Fragment = iota
	fragmentStart
	fragmentMiddle
	fragmentEnd
)

func (curr Fragment) isValidContinuation(prev Fragment) bool {
	switch prev {
	case fragmentUnfragmented, fragmentEnd:
		return curr == fragmentUnfragmented || curr == fragmentStart
	case fragmentStart, fragmentMiddle:
		return curr == fragmentMiddle || curr == fragmentEnd
	}
	panic("unreachable")
}

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

	defaultCloseCode = 1000
)

type Frame struct {
	payload []byte
	flags   byte
	opcode  OpCode
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

func (f Frame) fragment() Fragment {
	if f.Fin() {
		if f.opcode == Continuation {
			return fragmentEnd
		}
		return fragmentUnfragmented
	}
	if f.opcode == Continuation {
		return fragmentMiddle
	}
	// not fin and opcode binary or text
	return fragmentStart
}

func (f Frame) verifyContinuation(prev Fragment) error {
	if f.isControl() {
		return nil
	}
	if !f.fragment().isValidContinuation(prev) {
		return ErrInvalidFragmentation
	}
	return nil
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
	if f.Rsv2() || f.Rsv3() {
		return ErrReservedRsv
	}
	if err := f.opcode.verify(); err != nil {
		return err
	}
	if !f.isControl() {
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

func (f Frame) verifyRsvBits(deflateSupported bool) error {
	if f.Rsv1() && !deflateSupported {
		return ErrDeflateNotSupported
	}
	if f.Rsv2() || f.Rsv3() {
		return ErrReservedRsv
	}
	return nil
}

func (f Frame) isControl() bool {
	return f.opcode == Close ||
		f.opcode == Ping ||
		f.opcode == Pong
}

// newFrame uses buffered reader to decode frame.
//
// Blocks if there is not enough data in reader. Returns io.EOF when there is no
// frame in rdr. When last frame finishes on reader buffer boundary. Returns
// io.ErrUnexpectedEOF if frame parsing starts but then gets out of bytes.
//
// Returns:
//   - io.EOF - when no more frames in the stream, clean exit
//   - io.ErrUnexpectedEOF - when EOF happens in the middle of frame parsing, unclean exit
//   - (any other underlaying reader error)
//
// Note: ReadByte returns io.EOF when buffer emtpy, ReadFull returns ErrUnexpectedEOF!
func newFrame(rd *bufio.Reader) (Frame, error) {
	first, err := rd.ReadByte()
	if err != nil {
		return Frame{}, err
	}
	second, err := rd.ReadByte()
	if err != nil {
		if err == io.EOF {
			return Frame{}, io.ErrUnexpectedEOF
		}
		return Frame{}, err
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
		if _, err := io.ReadFull(rd, buf); err != nil {
			return Frame{}, err
		}
		payloadLen = uint64(binary.BigEndian.Uint16(buf))
	case 127:
		buf := make([]byte, 8)
		if _, err := io.ReadFull(rd, buf); err != nil {
			return Frame{}, err
		}
		payloadLen = binary.BigEndian.Uint64(buf)
	}

	// read masking key if present
	var mask []byte
	if masked {
		mask = make([]byte, 4)
		if _, err := io.ReadFull(rd, mask); err != nil {
			return Frame{}, err
		}
	}

	// read payload
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(rd, payload); err != nil {
			return Frame{}, err
		}

		if masked {
			maskUnmask(mask, payload)
		}
	}

	// create and verify frame
	frame := Frame{flags: flags, opcode: opcode, payload: payload}
	if err := frame.verify(); err != nil {
		return Frame{}, err
	}

	return frame, nil
}

func NewFrameFromBuffer(buf []byte) (Frame, error) {
	rdr := bufio.NewReader(bytes.NewReader(buf))
	return newFrame(rdr)
}

func maskUnmask(mask []byte, buf []byte) {
	for i, c := range buf {
		buf[i] = c ^ mask[i%4]
	}
}

type FrameReader struct {
	rd *bufio.Reader
}

func (r FrameReader) Read() (Frame, error) {
	return newFrame(r.rd)
}

func NewFrameReader(rd io.Reader) FrameReader {
	return FrameReader{rd: bufio.NewReader(rd)}
}

// TODO masked version
func (f Frame) Header() []byte {
	pbl := f.payloadLenBytes()
	header := make([]byte, 2+pbl)

	header[0] = finMask | byte(f.opcode)

	switch pbl {
	case 0:
		header[1] = byte(len(f.payload))
	case 2:
		header[1] = byte(126)
		binary.BigEndian.PutUint16(header[2:4], uint16(len(f.payload)))
	case 8:
		header[1] = byte(127)
		binary.BigEndian.PutUint64(header[2:10], uint64(len(f.payload)))
	}
	return header
}

func (f Frame) payloadLenBytes() int {
	len := len(f.payload)
	if len < 126 {
		return 0
	}
	if len < 65536 {
		return 2
	}
	return 8
}

func (f Frame) SendTo(w io.Writer) error {
	var buffers = net.Buffers([][]byte{f.Header(), f.payload})
	_, err := buffers.WriteTo(w)
	return err
}

func (f Frame) Buffers() net.Buffers {
	return net.Buffers([][]byte{f.Header(), f.payload})
}
