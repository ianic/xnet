package seb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

type EventType byte

const (
	LogEvent EventType = iota
	StateEvent
	DeltaEvent
)

type Event struct {
	Sequence  uint64
	Timestamp uint64
	Type      EventType
	Body      []byte
	// encoding
	// compression
	// key
}

type Operation byte

const (
	OpNone Operation = iota
	OpEvent
	// stream switch
	// pub
	// sub
	// connect
)

type Reader interface {
	ReadByte() (byte, error)
	Read(size int) ([]byte, error)
}

type Writer interface {
	Buffer(size int) ([]byte, error)
}

func (e *Event) FrameLen() int {
	// opcode, sequence, timestamp, type, body len, body bytes
	return 1 + 8 + 8 + 1 + 4 + len(e.Body)
}

func (e *Event) Encode(buf []byte) error {
	w := writer{buf: buf}
	w.PutByte(byte(OpEvent))
	w.PutUint64(e.Sequence)
	w.PutUint64(e.Timestamp)
	w.PutByte(byte(e.Type))
	w.PutSlice(e.Body)
	return w.Err()
}

type writer struct {
	buf    []byte
	offset int
	err    error
}

func (w *writer) Err() error {
	return w.err
}

func (w *writer) remainingSpace() int {
	return len(w.buf) - w.offset
}

func (w *writer) PutByte(v byte) {
	if w.err != nil {
		return
	}
	if w.remainingSpace() < 1 {
		w.err = ErrInsufficientBuffer
		return
	}
	w.buf[w.offset] = v
	w.offset += 1
}

func (w *writer) PutUint64(v uint64) {
	if w.err != nil {
		return
	}
	if w.remainingSpace() < 8 {
		w.err = ErrInsufficientBuffer
		return
	}
	binary.LittleEndian.PutUint64(w.buf[w.offset:w.offset+8], v)
	w.offset += 8
}

func (w *writer) PutSlice(v []byte) {
	if w.err != nil {
		return
	}
	l := len(v)
	// TODO maybe check max l and return error
	if w.remainingSpace() < l+4 {
		w.err = ErrInsufficientBuffer
		return
	}

	binary.LittleEndian.PutUint32(w.buf[w.offset:w.offset+4], uint32(l))
	w.offset += 4
	if copy(w.buf[w.offset:], v) != l {
		w.err = ErrInsufficientBuffer
		return
	}
	w.offset += l
}

type reader struct {
	buf    []byte
	offset int
	err    error
}

func (r *reader) remainingSpace() int {
	return len(r.buf) - r.offset
}

func (r *reader) Byte() byte {
	if r.err != nil {
		return 0
	}
	if r.remainingSpace() < 1 {
		return 0
	}
	v := r.buf[r.offset]
	r.offset += 1
	return v
}

func (r *reader) Uint64() uint64 {
	if r.err != nil {
		return 0
	}
	const l = 8
	if r.remainingSpace() < l {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.buf[r.offset : r.offset+l])
	r.offset += l
	return v
}

func (r *reader) Uint32() uint32 {
	if r.err != nil {
		return 0
	}
	const l = 4
	if r.remainingSpace() < l {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.buf[r.offset : r.offset+l])
	r.offset += l
	return v
}

func (r *reader) Slice() []byte {
	if r.err != nil {
		return nil
	}
	l := int(r.Uint32())
	if l == 0 {
		return nil
	}
	if r.remainingSpace() < l {
		return nil
	}
	v := r.buf[r.offset : r.offset+l]
	r.offset += l
	return v
}

func (r *reader) Err() error {
	return r.err
}

func (e *Event) Decode(buf []byte) error {
	r := reader{buf: buf}
	op := Operation(r.Byte())
	if op != OpEvent {
		return ErrWrongOperation
	}
	e.Sequence = r.Uint64()
	e.Timestamp = r.Uint64()
	e.Type = EventType(r.Byte())
	e.Body = r.Slice()
	return r.Err()
}

// func (e *Event) Decode(rd Reader) error {
// 	// check opcode
// 	opcode, err := rd.ReadByte()
// 	if err != nil {
// 		return err
// 	}
// 	if opcode != byte(OpEvent) {
// 		return ErrWrongOperation
// 	}
// 	// sequence
// 	buf, err := rd.Read(8)
// 	if err != nil {
// 		return err
// 	}
// 	e.Sequence = binary.LittleEndian.Uint64(buf)
// 	// timestamp
// 	buf, err = rd.Read(8)
// 	if err != nil {
// 		return err
// 	}
// 	e.Timestamp = binary.LittleEndian.Uint64(buf)
// 	// type
// 	typ, err := rd.ReadByte()
// 	if err != nil {
// 		return err
// 	}
// 	e.Type = EventType(typ)

// 	// body
// 	buf, err = rd.Read(4)
// 	if err != nil {
// 		return err
// 	}
// 	bodyLen := binary.LittleEndian.Uint32(buf)
// 	body, err := rd.Read(int(bodyLen))
// 	if err != nil {
// 		return err
// 	}
// 	e.Body = body
// 	return nil
// }

var (
	ErrInsufficientBuffer = errors.New("insufficient buffer")
	ErrWrongOperation     = errors.New("wrong operation")
)

type BufferReader struct {
	buf  []byte
	head int
	tail int
}

func (r *BufferReader) ReadByte() (byte, error) {
	if r.head < len(r.buf) {
		b := r.buf[r.head]
		r.head++
		return b, nil
	}
	if r.head == r.tail {
		return 0, io.EOF
	}
	return 0, &ErrReadMore{Bytes: 1}
}

func (r *BufferReader) Read(size int) ([]byte, error) {
	if r.head+size <= len(r.buf) {
		b := r.buf[r.head : r.head+size]
		r.head += size
		return b, nil
	}
	if r.head == r.tail {
		return nil, io.EOF
	}
	return nil, &ErrReadMore{Bytes: r.head + size - len(r.buf)}
}

func (r *BufferReader) pending() []byte {
	return r.buf[r.tail:]
}

func NewBufferReader(buf []byte) *BufferReader {
	return &BufferReader{buf: buf}
}

type ErrReadMore struct {
	Bytes int
}

func (e *ErrReadMore) Error() string {
	return fmt.Sprintf("read %d bytes more", e.Bytes)
}
