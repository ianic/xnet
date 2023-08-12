package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	hs, err := Parse([]byte(http_request))
	assert.NoError(t, err)

	assert.Equal(t, "13", hs.version)
	assert.Equal(t, "3yMLSWFdF1MH1YDDPW/aYQ==", hs.key)
	assert.Equal(t, "ws.example.com", hs.host)

	assert.True(t, hs.extension.permessageDeflate)
	assert.True(t, hs.extension.serverMaxWindowBits.included)
	assert.True(t, hs.extension.clientMaxWindowBits.included)
	assert.Equal(t, 12, hs.extension.serverMaxWindowBits.value)
	assert.Equal(t, 13, hs.extension.clientMaxWindowBits.value)

}

const http_request = "GET ws://ws.example.com/ws HTTP/1.1\r\n" +
	"Host: ws.example.com\r\n" +
	"Upgrade: websocket\r\n" +
	"Connection: Upgrade\r\n" +
	"Sec-WebSocket-Key: 3yMLSWFdF1MH1YDDPW/aYQ==\r\n" +
	"Sec-WebSocket-Version: 13\r\n" +
	"Sec-WebSocket-Extensions: permessage-deflate; server_max_window_bits=12; client_max_window_bits=13\r\n\r\n"
