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

func (h *testHandler) Disconnected(error) {}

type testStream struct {
	sent       [][]byte
	closeCalls int
	closeError error
}

func (s *testStream) Send(data []byte) error {
	s.sent = append(s.sent, data)
	return nil
}

func (s *testStream) Close(err error) {
	s.closeError = err
	s.closeCalls++
}

func TestAsyncConnParseMessage(t *testing.T) {
	h := testHandler{}
	s := testStream{}

	c := AsyncConn{stream: &s, handler: &h}

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
	if len(c.payload) != 0 ||
		c.opcode != None {
		t.Fatal("unexpected message state")
	}
	if len(c.pending) != 7 ||
		c.recvMore != 4 {
		t.Fatalf("unexpected parsing state %d %d", len(c.pending), c.recvMore)
	}
	// push some more
	c.Received(maskedHelloFrame[7:9])
	if len(c.pending) != 9 ||
		c.recvMore != 2 {
		t.Fatalf("unexpected parsing state %d %d", len(c.pending), c.recvMore)
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
	c := AsyncConn{stream: &s, handler: &h}

	c.Received(fragment1[:2])
	if len(c.pending) != 2 {
		t.Fatalf("unexpected pending len %d", len(c.pending))
	}
	c.Received(fragment1[2:])
	if len(c.payload) != 1 ||
		c.opcode != Text ||
		c.prevFrameFragment != fragFirst {
		t.Fatal()
	}
	c.Received(pingFrame[:1])
	if len(c.pending) != 1 {
		t.Fatalf("unexpected pending len %d", len(c.pending))
	}
	c.Received(pingFrame[1:])
	c.Received(fragment2[:3])
	if len(c.pending) != 3 {
		t.Fatalf("unexpected pending len %d", len(c.pending))
	}
	c.Received(fragment2[3:])
	if len(c.payload) != 4 {
		t.Fatalf("unexpected pending len %d", len(c.pending))
	}
	c.Received(pongFrame[:1])
	if len(c.pending) != 1 {
		t.Fatalf("unexpected pending len %d", len(c.pending))
	}
	c.Received(pongFrame[1:])
	c.Received(fragment3[:1])
	if len(c.pending) != 1 {
		t.Fatalf("unexpected pending len %d", len(c.pending))
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
1
