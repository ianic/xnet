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

func (s *testStream) Send(data []byte) {
	s.sent = append(s.sent, data)
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
	if len(c.ms.payload) != 0 ||
		c.ms.opcode != None {
		t.Fatal("unexpected message state")
	}
	if len(c.fs.pending) != 7 ||
		c.fs.recvMore != 4 {
		t.Fatalf("unexpected parsing state %d %d", len(c.fs.pending), c.fs.recvMore)
	}
	// push some more
	c.Received(maskedHelloFrame[7:9])
	if len(c.fs.pending) != 9 ||
		c.fs.recvMore != 2 {
		t.Fatalf("unexpected parsing state %d %d", len(c.fs.pending), c.fs.recvMore)
	}
	// push the rest
	c.Received(maskedHelloFrame[9:])
	if len(h.received) != 2 ||
		string(h.received[1]) != "Hello" {
		t.Fatalf("unexpected handler calls %d", len(h.received))
	}
}

func TestAsyncConnParseFragmentedMessage(t *testing.T) {
	h := testHandler{}
	s := testStream{}
	c := AsyncConn{tc: &s, up: &h}

	c.Received(fragment1[:2])
	if len(c.fs.pending) != 2 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	c.Received(fragment1[2:])
	if len(c.ms.payload) != 1 ||
		c.ms.opcode != Text ||
		c.ms.prevFrameFragment != fragFirst {
		t.Fatal()
	}
	c.Received(pingFrame[:1])
	if len(c.fs.pending) != 1 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	c.Received(pingFrame[1:])
	c.Received(fragment2[:3])
	if len(c.fs.pending) != 3 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	c.Received(fragment2[3:])
	if len(c.ms.payload) != 4 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	c.Received(pongFrame[:1])
	if len(c.fs.pending) != 1 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	c.Received(pongFrame[1:])
	c.Received(fragment3[:1])
	if len(c.fs.pending) != 1 {
		t.Fatalf("unexpected pending len %d", len(c.fs.pending))
	}
	c.Received(fragment3[1:])

	if len(h.received) != 1 ||
		string(h.received[0]) != "Hello!" {
		t.Fatalf("unexpected upstream communication messages: %d", len(h.received))
	}

	if len(s.sent) != 1 || !bytes.Equal(s.sent[0], pongFrame) {
		t.Fatalf("unexpected downstream send %d", len(s.sent))
	}
}
