package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	helloFrame       = []byte{0x81, 0x05, 0x48, 0x65, 0x6c, 0x6c, 0x6f}
	maskedHelloFrame = []byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}
	pingFrame        = []byte{0x89, 0x00}
	pongFrame        = []byte{0x8a, 0x00}
)

func TestNewFrame(t *testing.T) {
	f, err := NewFrame(helloFrame)
	assert.NoError(t, err)
	assert.True(t, f.Fin())
	assert.False(t, f.Rsv1())
	assert.False(t, f.Rsv2())
	assert.False(t, f.Rsv3())
	assert.Equal(t, Text, f.opcode)
	assert.Equal(t, []byte("Hello"), f.payload)

	f, err = NewFrame(maskedHelloFrame)
	require.NoError(t, err)
	assert.True(t, f.Fin())
	assert.False(t, f.Rsv1())
	assert.False(t, f.Rsv2())
	assert.False(t, f.Rsv3())
	assert.Equal(t, Text, f.opcode)
	assert.Equal(t, []byte("Hello"), f.payload)
}

func TestNewFrameOpcode(t *testing.T) {
	f, err := NewFrame(helloFrame)
	assert.NoError(t, err)
	assert.Equal(t, Text, f.opcode)

	f, err = NewFrame(pingFrame)
	assert.NoError(t, err)
	assert.Equal(t, Ping, f.opcode)

	f, err = NewFrame(pongFrame)
	assert.NoError(t, err)
	assert.Equal(t, Pong, f.opcode)
}
