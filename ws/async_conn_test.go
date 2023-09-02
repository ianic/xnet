package ws

import (
	"bytes"
	"testing"
)

type testHandler struct {
	received [][]byte
}

func (h *testHandler) Received(data []byte) {
	h.received = append(h.received, data)
}

func (h *testHandler) Closed(error) {}
func (h *testHandler) Sent()        {}

type testStream struct {
	sent       [][]byte
	closeCalls int
	closeError error
}

func (s *testStream) SendBuffers(buffers [][]byte) {
	s.sent = append(s.sent, buffers...)
}

func (s *testStream) Close() {
	s.closeCalls++
}

func TestAsyncConnParseMessage(t *testing.T) {
	h := testHandler{}
	s := testStream{}

	c := AsyncConn{tc: &s, up: &h}

	// push hello frame
	c.Received(helloFrame)
	if len(h.received) != 1 ||
		string(h.received[0]) != "Hello" {
		t.Fatal()
	}
	// push part of the masked hello frame
	c.Received(maskedHelloFrame[:7])
	if len(h.received) != 1 || s.closeCalls != 0 {
		t.Fatalf("unexpected communication %d %d %s", len(h.received), s.closeCalls, s.closeError)
	}
	if c.partialFrame != nil {
		t.Fatal("unexpected partial frame")
	}
	if len(c.fs.pending) != 7 ||
		c.fs.recvMore != 4 {
		t.Fatalf("unexpected parsing state %d %d", len(c.fs.pending), c.fs.recvMore)
	}
	// push some more
	c.Received(maskedHelloFrame[7:9])
	if c.partialFrame != nil {
		t.Fatal("unexpected partial frame")
	}
	if len(c.fs.pending) != 9 ||
		c.fs.recvMore != 2 {
		t.Fatalf("unexpected parsing state %d %d", len(c.fs.pending), c.fs.recvMore)
	}
	// push the rest
	c.Received(maskedHelloFrame[9:])
	if c.partialFrame != nil {
		t.Fatal("unexpected partial frame")
	}
	if len(h.received) != 2 ||
		string(h.received[1]) != "Hello" {
		t.Fatalf("unexpected handler calls %d", len(h.received))
	}
}

func TestAsyncConnParseFragmentedMessage(t *testing.T) {
	h := testHandler{}

	s := testStream{}
	c := AsyncConn{tc: &s, up: &h}

	// part of fragment 1
	c.Received(fragment1[:2])
	if len(c.fs.pending) != 2 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	if c.partialFrame != nil {
		t.Fatal("unexpected partial frame")
	}
	// second part of fragment1
	c.Received(fragment1[2:])
	if len(c.partialFrame.payload) != 1 ||
		c.partialFrame.opcode != Text {
		t.Fatal()
	}
	// part of ping
	c.Received(pingFrame[:1])
	if len(c.fs.pending) != 1 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	// second part of ping
	c.Received(pingFrame[1:])
	// test that pong is sent
	if len(s.sent) != 2 || !bytes.Equal(s.sent[0], pongFrame) {
		t.Fatalf("unexpected downstream send %d", len(s.sent))
	}

	// part of fragment2
	c.Received(fragment2[:3])
	if len(c.fs.pending) != 3 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	// rest of fragment2
	c.Received(fragment2[3:])
	if len(c.partialFrame.payload) != 4 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}

	// pong
	c.Received(pongFrame[:1])
	if len(c.fs.pending) != 1 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	// rest of pong, first part of fragment3
	c.Received(pongFrame[1:])
	c.Received(fragment3[:1])
	if len(c.fs.pending) != 1 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	// rest of fragment 3
	c.Received(fragment3[1:])

	// finally received full frame
	if len(h.received) != 1 ||
		string(h.received[0]) != "Hello!" {
		t.Fatalf("unexpected upstream communication messages: %d", len(h.received))
	}

	// replay was only one pong
	if len(s.sent) != 2 || !bytes.Equal(s.sent[0], pongFrame) {
		t.Fatalf("unexpected downstream send %d", len(s.sent))
	}
}
