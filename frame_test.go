package ws

import (
	"io"
	"testing"
)

var (
	helloFrame       = []byte{0x81, 0x05, 0x48, 0x65, 0x6c, 0x6c, 0x6f}
	maskedHelloFrame = []byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}
	pingFrame        = []byte{0x89, 0x00}
	pongFrame        = []byte{0x8a, 0x00}
	closeFrame       = []byte{0x88, 0x02, 0x03, 0xe9} // default close frame, status 1001

	fragment1 = []byte{0x01, 0x1, 0x48}             // first text frame
	fragment2 = []byte{0x00, 0x3, 0x65, 0x6c, 0x6c} // continuation frame
	fragment3 = []byte{0x80, 0x2, 0x6f, 0x21}       // last continuation frame

	fragmentedMessage = append(append(append(append(
		fragment1,
		pingFrame...),
		fragment2...),
		pongFrame...),
		fragment3...)
)

func TestNewFrame(t *testing.T) {
	cases := []struct {
		data      []byte
		expected  Frame
		closeCode uint16
	}{
		{helloFrame, Frame{fin: true, opcode: Text, payload: []byte("Hello")}, 0},
		{maskedHelloFrame, Frame{fin: true, opcode: Text, payload: []byte("Hello")}, 0},
		{pingFrame, Frame{fin: true, opcode: Ping}, 0},
		{pongFrame, Frame{fin: true, opcode: Pong}, 0},
		{closeFrame, Frame{fin: true, opcode: Close, payload: []byte{0x03, 0xe9}}, 1001},
	}

	for i, c := range cases {
		f, err := NewFrameFromBuffer(c.data)
		if err != nil {
			t.Fatal(err)
		}
		if c.expected.fin != f.fin ||
			c.expected.opcode != f.opcode ||
			string(c.expected.payload) != string(f.payload) {
			t.Fatalf("case %d %v != %v", i, f, c.expected)
		}
		if c.closeCode != f.closeCode() {
			t.Fatalf("case %d unexpected close code %d", i, f.closeCode())
		}
	}
}

func TestParseFragmentedMessage(t *testing.T) {
	rdr := NewFrameReaderFromBuffer(fragmentedMessage)

	frags := []struct {
		opcode OpCode
		fin    bool
	}{
		{Text, false},
		{Ping, true},
		{Continuation, false},
		{Pong, true},
		{Continuation, true},
	}

	var payload []byte
	for i, e := range frags {
		a, err := rdr.Read()
		if err != nil {
			t.Fatal(err)
		}
		if e.fin != a.fin ||
			e.opcode != a.opcode {
			t.Fatalf("unexpected fragment %d", i)
		}
		if a.opcode == Text || a.opcode == Continuation {
			payload = append(payload, a.payload...)
		}

	}
	if string(payload) != "Hello!" {
		t.Fatalf("payload")
	}

	_, err := rdr.Read()
	if err != io.EOF {
		t.Fatal(err)
	}
}
