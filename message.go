package main

import (
	"io"
	"unicode/utf8"
)

// WebSocket connection
type Connection struct {
	conn io.ReadWriteCloser // underlaying network connection
	rd   FrameReader
}

func NewConnection(conn io.ReadWriteCloser) Connection {
	return Connection{conn: conn, rd: NewFrameReader(conn)}
}

type MessageEncoding byte

const (
	EncodingText   MessageEncoding = 1
	EncodingBinary MessageEncoding = 2
)

type Message struct {
	Payload  []byte
	Encoding MessageEncoding
}

func (m *Message) append(frame Frame) {
	if m.Encoding == 0 {
		m.Encoding = MessageEncoding(frame.opcode)
	}
	if len(m.Payload) == 0 {
		m.Payload = frame.payload
		return
	}
	m.Payload = append(m.Payload, frame.payload...)
}

func (m *Message) verify() error {
	if m.Encoding == EncodingText && !utf8.Valid(m.Payload) {
		return ErrInvalidUtf8Payload
	}
	return nil
}

func (msg Message) SendTo(w io.Writer) error {
	frame := Frame{opcode: OpCode(msg.Encoding), payload: msg.Payload}
	return frame.SendTo(w)
}

func (c *Connection) Read() (Message, error) {
	var msg Message
	fragment := fragmentUnfragmented
	for {
		frame, err := c.rd.Read()
		if err != nil {
			return Message{}, err
		}
		if frame.isControl() {
			if err := c.handleControl(frame); err != nil {
				return Message{}, err
			}
			if frame.opcode == Close {
				return Message{}, io.EOF
			}
			continue
		}

		if err := frame.verifyContinuation(fragment); err != nil {
			return Message{}, err
		}
		// TODO set real deflate flag
		if err := frame.verifyRsvBits(false); err != nil {
			return Message{}, err
		}
		fragment = frame.fragment()

		msg.append(frame)
		if frame.Fin() {
			if fragment == fragmentEnd {
				// Verify only if message if compposed of multiple frames. Frame
				// is already verified. So if message is from single frame
				// everyting is already verified.
				if err := msg.verify(); err != nil {
					return Message{}, err
				}
			}
			return msg, nil
		}
	}
}

func (c *Connection) handleControl(frame Frame) error {
	switch frame.opcode {
	case Ping:
		pong := Frame{opcode: Pong, payload: frame.payload}
		return pong.SendTo(c.conn)
	case Pong:
		// nothing to do on pong
		return nil
	case Close:
		return frame.SendTo(c.conn)
	default:
		panic("not a control frame")
	}
}
