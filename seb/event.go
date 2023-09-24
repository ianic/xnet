package seb

import (
	"encoding/binary"
	"errors"
)

var (
	ErrInsufficientBuffer = errors.New("insufficient buffer")
	ErrWrongOperation     = errors.New("wrong operation")
	ErrSplitBuffer        = errors.New("split buffer")
)

type Operation byte

const (
	OpNone Operation = iota
	OpEvent
	// stream switch
	// pub
	// sub
	// connect
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

func (e *Event) FrameLen() int {
	// opcode, sequence, timestamp, type, body len, body bytes
	return 1 + 8 + 8 + 1 + 4 + len(e.Body)
}

func (e *Event) Encode(buf []byte) error {
	w := writer{buf: buf}
	return e.encode(&w)
}

func (e *Event) encode(w *writer) error {
	w.PutByte(byte(OpEvent))
	w.PutUint64(e.Sequence)
	w.PutUint64(e.Timestamp)
	w.PutByte(byte(e.Type))
	w.PutSlice(e.Body)
	return w.Done()
}

func (e *Event) Decode(buf []byte) error {
	r := reader{buf: buf}
	return e.decode(&r)
}

func (e *Event) decode(r *reader) error {
	op := Operation(r.Byte())
	if op != OpEvent {
		return ErrWrongOperation
	}
	e.Sequence = r.Uint64()
	e.Timestamp = r.Uint64()
	e.Type = EventType(r.Byte())
	e.Body = r.Slice()
	return r.Done()
}

type writer struct {
	buf  []byte
	head int
	tail int
	err  error
}

func (w *writer) Done() error {
	if w.err == nil {
		w.head = w.tail
	}
	return w.err
}

// Written part of the writer buffer.
// Part of the w.buf where write finished.
func (w *writer) Written() []byte {
	return w.buf[0:w.head]
}

func (w *writer) reset() {
	w.tail = w.head
	w.err = nil
}

func (w *writer) available() int {
	return len(w.buf) - w.tail
}

func (w *writer) enoughSpace(l int) bool {
	if w.err != nil {
		return false
	}
	if w.available() < l {
		w.err = ErrInsufficientBuffer
		return false
	}
	return true
}

func (w *writer) PutByte(v byte) {
	if !w.enoughSpace(1) {
		return
	}
	w.buf[w.tail] = v
	w.tail += 1
}

func (w *writer) PutUint64(v uint64) {
	const l = 8
	if !w.enoughSpace(l) {
		return
	}
	binary.LittleEndian.PutUint64(w.buf[w.tail:w.tail+l], v)
	w.tail += l
}

func (w *writer) PutUint32(v uint32) {
	const l = 4
	if !w.enoughSpace(l) {
		return
	}
	binary.LittleEndian.PutUint32(w.buf[w.tail:w.tail+l], v)
	w.tail += l
}

func (w *writer) PutSlice(v []byte) {
	l := len(v)
	w.PutUint32(uint32(l))
	if !w.enoughSpace(l) {
		return
	}
	if copy(w.buf[w.tail:w.tail+l], v) != l {
		w.err = ErrInsufficientBuffer
		return
	}
	w.tail += l
}

type reader struct {
	buf  []byte
	head int
	tail int
	err  error
}

func (r *reader) Done() error {
	if r.err == nil {
		r.head = r.tail
	}
	return r.err
}

func (r *reader) Unread() []byte {
	return r.buf[r.head:]
}

func (r *reader) reset() {
	r.tail = r.head
	r.err = nil
}

func (r *reader) available() int {
	return len(r.buf) - r.tail
}

func (r *reader) enoughSpace(l int) bool {
	if r.err != nil {
		return false
	}
	if l > r.available() {
		r.err = ErrSplitBuffer
		return false
	}
	return true
}

func (r *reader) Byte() byte {
	if !r.enoughSpace(1) {
		return 0
	}
	v := r.buf[r.tail]
	r.tail += 1
	return v
}

func (r *reader) Uint64() uint64 {
	const l = 8
	if !r.enoughSpace(l) {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.buf[r.tail : r.tail+l])
	r.tail += l
	return v
}

func (r *reader) Uint32() uint32 {
	const l = 4
	if !r.enoughSpace(l) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.buf[r.tail : r.tail+l])
	r.tail += l
	return v
}

// TODO Slice32 Slice16 methods for different slice len
func (r *reader) Slice() []byte {
	l := int(r.Uint32())
	if l == 0 || !r.enoughSpace(l) {
		return nil
	}
	v := r.buf[r.tail : r.tail+l]
	r.tail += l
	return v
}
