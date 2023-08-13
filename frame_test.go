package main

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	helloFrame       = []byte{0x81, 0x05, 0x48, 0x65, 0x6c, 0x6c, 0x6f}
	maskedHelloFrame = []byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}
	pingFrame        = []byte{0x89, 0x00}
	pongFrame        = []byte{0x8a, 0x00}

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

func TestParseFragmentedMessage(t *testing.T) {
	rdr := bufio.NewReader(bytes.NewReader(fragmentedMessage))
	iter := NewIterator(rdr)

	f1 := iter.Next()
	require.NotNil(t, f1)
	assert.Equal(t, f1.opcode, Text)
	assert.False(t, f1.Fin())

	fp := iter.Next()
	require.NotNil(t, fp)
	assert.Equal(t, fp.opcode, Ping)
	assert.True(t, fp.Fin())

	f2 := iter.Next()
	require.NotNil(t, f2)
	assert.Equal(t, f2.opcode, Continuation)
	assert.False(t, f1.Fin())

	fo := iter.Next()
	require.NotNil(t, fo)
	assert.Equal(t, fo.opcode, Pong)
	assert.True(t, fo.Fin())

	f3 := iter.Next()
	require.NotNil(t, f3)
	assert.Equal(t, f3.opcode, Continuation)
	assert.True(t, f3.Fin())

	assert.Equal(t, "Hello!", string(f1.payload)+string(f2.payload)+string(f3.payload))

	require.Nil(t, iter.Next())
	require.Nil(t, iter.Next())
	require.Nil(t, iter.err)
}
