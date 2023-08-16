package ws

import (
	"bytes"
	"compress/flate"
	"io"
	"net"
	"unicode/utf8"
)

// WebSocket connection
type Conn struct {
	conn      io.ReadWriter // underlaying network connection
	rd        FrameReader
	extension Extension
	//decompressorReader *bytes.Reader
	decompressor io.ReadCloser
	compressor   *flate.Writer
}

func NewConnection(conn io.ReadWriter, extension Extension) Conn {
	ws := Conn{
		conn:      conn,
		rd:        NewFrameReader(conn),
		extension: extension,
	}
	if extension.permessageDeflate {
		//ws.decompressorReader = bytes.NewReader(nil)
		ws.decompressor = flate.NewReader(nil)
		ws.compressor, _ = flate.NewWriter(nil, 7)
	}
	return ws
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

func (msg Message) Buffers() net.Buffers {
	frame := Frame{opcode: OpCode(msg.Encoding), payload: msg.Payload}
	return frame.Buffers()
}

func (c *Conn) Read() (Message, error) {
	var msg Message
	fragment := fragmentUnfragmented
	compressed := false
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
		if err := frame.verifyRsvBits(c.extension.permessageDeflate); err != nil {
			return Message{}, err
		}
		fragment = frame.fragment()
		if fragment == fragmentStart || fragment == fragmentUnfragmented {
			compressed = frame.Rsv1()
		}

		msg.append(frame)
		if frame.Fin() {
			if compressed {
				msg.Payload, err = c.decompress(msg.Payload)
				if err != nil {
					return Message{}, err
				}
			}
			if err := msg.verify(); err != nil {
				return Message{}, err
			}

			return msg, nil
		}
	}
}

func (c *Conn) decompress(payload []byte) ([]byte, error) {
	dc := decompressors.Get().(*Decompressor)
	defer decompressors.Put(dc)
	return dc.decompress(payload)
}

func (c *Conn) compress(payload []byte) ([]byte, error) {
	cp := compressors.Get().(*Compressor)
	defer compressors.Put(cp)
	return cp.compress(payload)
}

func (c *Conn) handleControl(frame Frame) error {
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

func (c *Conn) Write(msg Message) error {
	payload := msg.Payload
	if c.extension.permessageDeflate {
		var err error
		payload, err = c.compress(payload)
		if err != nil {
			return err
		}
	}
	frame := Frame{opcode: OpCode(msg.Encoding), payload: payload}
	buffers := frame.Buffers()
	// TODO ugly
	if c.extension.permessageDeflate {
		buffers[0][0] |= rsv1Mask // set rsv1 bit
	}
	_, err := buffers.WriteTo(c.conn)
	return err
}

var compressLastBlock = []byte{0x00, 0x00, 0xff, 0xff, 0x01, 0x00, 0x00, 0xff, 0xff}

func decompress(data []byte) ([]byte, error) {
	d := flate.NewReader(bytes.NewReader(append(data, compressLastBlock...)))
	return io.ReadAll(d)
}

func compress(data []byte) ([]byte, error) {
	buf := &bytes.Buffer{}
	//cp, err := flate.NewWriter(buf, flate.BestCompression)
	//cp, err := flate.NewWriterWindow(buf, 1<<15)
	cp, err := flate.NewWriter(buf, 7)
	if err != nil {
		return nil, err
	}
	_, err = cp.Write(data)
	if err != nil {
		return nil, err
	}
	if err := cp.Flush(); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	return b[:len(b)-4], nil
}
