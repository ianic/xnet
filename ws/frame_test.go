package ws

import (
	"bufio"
	"bytes"
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
	t.Run("with buffered reader", func(t *testing.T) {
		testNewFrame(t, func(buf []byte) (Frame, error) {
			rdr := newBufioBytesReader(bufio.NewReader(bytes.NewReader(buf)))
			return newFrame(rdr)
		})
	})

	t.Run("with bytes reader", func(t *testing.T) {
		testNewFrame(t, func(buf []byte) (Frame, error) {
			return newFrame(&bufferBytesReader{buf: buf})
		})
	})
}

func testNewFrame(t *testing.T, newFrame func([]byte) (Frame, error)) {

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

	assert := func(expectedFrame Frame, expectedCloseCode uint16, actualFrame Frame, caseNo int) {
		if expectedFrame.fin != actualFrame.fin ||
			expectedFrame.opcode != actualFrame.opcode ||
			string(expectedFrame.payload) != string(actualFrame.payload) {
			t.Fatalf("case %d %v != %v", caseNo, actualFrame, expectedFrame)
		}
		if expectedCloseCode != actualFrame.closeCode() {
			t.Fatalf("case %d unexpected close code %d", caseNo, actualFrame.closeCode())
		}
	}

	for caseNo, c := range cases {
		actualFrame, err := newFrame(c.data)
		if err != nil {
			t.Fatal(err)
		}
		assert(c.expected, c.closeCode, actualFrame, caseNo)
	}
}

func TestParseFragmentedMessage(t *testing.T) {
	t.Run("with buffered reader", func(t *testing.T) {
		testParseFragmentedMessage(t, func(buf []byte) FrameReader {
			br := bufio.NewReader(bytes.NewReader(fragmentedMessage))
			return NewFrameReader(br)
		})
	})

	t.Run("with bytes reader", func(t *testing.T) {
		testParseFragmentedMessage(t, NewFrameReaderFromBuffer)
	})
}

func testParseFragmentedMessage(t *testing.T, frameFactory func([]byte) FrameReader) {
	rdr := frameFactory(fragmentedMessage)
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

func TestReadMore(t *testing.T) {
	_, err := newFrame(&bufferBytesReader{buf: maskedHelloFrame[:2]})
	rm := err.(*ErrReadMore)
	if rm.Bytes != 4 {
		t.Fatalf("expect to read 4 bytes of mask")
	}

	_, err = newFrame(&bufferBytesReader{buf: maskedHelloFrame[:6]})
	rm = err.(*ErrReadMore)
	if rm.Bytes != 5 {
		t.Fatalf("expect to read 5 bytes of payload")
	}

	// fragmentedMessage split at fragment2
	// 3 bytes of fragment1, 2 bytes of ping + 2 bytes of fragment2
	fr := NewFrameReaderFromBuffer(fragmentedMessage[:7])
	_, err = fr.Read()
	if err != nil {
		t.Fatal(err)
	}
	_, err = fr.Read()
	if err != nil {
		t.Fatal(err)
	}
	_, err = fr.Read()
	rm = err.(*ErrReadMore)
	if rm.Bytes != 3 {
		t.Fatalf("expect to read 3 bytes of payload")
	}
}

func TestDefragmentSingleFrame(t *testing.T) {
	hello := frameFrom(t, helloFrame)

	var full *Frame = nil
	full, partail, err := full.defragment(hello)
	if err != nil {
		t.Fatal(err)
	}
	if partail != nil {
		t.Fatalf("should be no partial")
	}
	if !full.fin || full.opcode != Text || len(full.payload) != 5 {
		t.Fatalf("unexpected frame state")
	}
	_, _, err = full.defragment(hello)
	if err != ErrInvalidFragmentation {
		t.Fatalf("can't append to fin frame %s", err)
	}
}

func TestDefragmentFragmentedMessage(t *testing.T) {
	var full *Frame = nil
	var partial *Frame = nil
	var err error

	full, partial, err = partial.defragment(frameFrom(t, fragment1))
	if err != nil {
		t.Fatal(err)
	}
	if full != nil || partial == nil {
		t.Fatalf("unexpecte full")
	}
	if partial.fin || len(partial.payload) != 1 || partial.opcode != Text ||
		partial.fragment() != fragFirst {
		t.Fatalf("unexpected frame state")
	}

	full, partial, err = partial.defragment(frameFrom(t, fragment2))
	if err != nil {
		t.Fatal(err)
	}
	if full != nil {
		t.Fatalf("unexpecte full")
	}
	if partial.fin || len(partial.payload) != 4 || partial.opcode != Text ||
		partial.fragment() != fragFirst {
		t.Fatalf("unexpected frame state")
	}

	full, partial, err = partial.defragment(frameFrom(t, fragment3))
	if err != nil {
		t.Fatal(err)
	}
	if full == nil || partial != nil {
		t.Fatalf("expected full")
	}
	if !full.fin ||
		len(full.payload) != 6 ||
		full.opcode != Text ||
		full.fragment() != fragSingle {
		t.Fatalf("unexpected frame state")
	}
}

func frameFrom(t *testing.T, buf []byte) *Frame {
	fr, err := newFrame(&bufferBytesReader{buf: buf})
	if err != nil {
		t.Fatal(err)
	}
	return &fr
}
