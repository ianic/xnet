package ws

import (
	"bufio"
	"strings"
	"testing"
)

func TestHandshake(t *testing.T) {
	hs, err := NewHandshake(bufio.NewReader(strings.NewReader(testRequest)))
	if err != nil {
		t.Fatal(err)
	}

	if hs.version != "13" ||
		hs.key != "3yMLSWFdF1MH1YDDPW/aYQ==" ||
		hs.host != "ws.example.com" {
		t.Fatalf("basic headers")
	}

	if !hs.extension.permessageDeflate ||
		hs.extension.serverMaxWindowBits != 12 ||
		hs.extension.clientMaxWindowBits != 13 {
		t.Fatalf("extension header")
	}

	const expected = "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: 9bQuZIN64KrRsqgxuR1CxYN94zQ=\r\n" +
		"Sec-WebSocket-Extensions: permessage-deflate; client_no_context_takeover; server_no_context_takeover\r\n\r\n"
	if hs.Response() != expected {
		t.Fatalf("unexpected response")
	}
}

func TestSecKey(t *testing.T) {
	key, err := secKey()
	if err != nil ||
		len(key) != 24 {
		t.Fatal()
	}
}

func TestSecAccept(t *testing.T) {
	cases := []struct {
		key       string
		acceptKey string
	}{
		{"dGhlIHNhbXBsZSBub25jZQ==", "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
		{"3yMLSWFdF1MH1YDDPW/aYQ==", "9bQuZIN64KrRsqgxuR1CxYN94zQ="},
		{"/Hua7JHfD1waXr47jL/uAg==", "ELgfPf42E81xadzWVke1JyXNmqU="},
	}
	for i, c := range cases {
		if c.acceptKey != secAccept((c.key)) {
			t.Fatalf("unexpected accept key in case %d", i)
		}
	}
}

const testRequest = "GET ws://ws.example.com/ws HTTP/1.1\r\n" +
	"Host: ws.example.com\r\n" +
	"Upgrade: websocket\r\n" +
	"Connection: Upgrade\r\n" +
	"Sec-WebSocket-Key: 3yMLSWFdF1MH1YDDPW/aYQ==\r\n" +
	"Sec-WebSocket-Version: 13\r\n" +
	"Sec-WebSocket-Extensions: permessage-deflate; server_max_window_bits=12; client_max_window_bits=13\r\n\r\n"
