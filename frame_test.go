package ws

import (
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
	f, err := NewFrameFromBuffer(helloFrame)
	assert.NoError(t, err)
	assert.True(t, f.fin)
	assert.False(t, f.rsv1())
	assert.False(t, f.rsv2())
	assert.False(t, f.rsv3())
	assert.Equal(t, Text, f.opcode)
	assert.Equal(t, []byte("Hello"), f.payload)

	f, err = NewFrameFromBuffer(maskedHelloFrame)
	require.NoError(t, err)
	assert.True(t, f.fin)
	assert.False(t, f.rsv1())
	assert.False(t, f.rsv2())
	assert.False(t, f.rsv3())
	assert.Equal(t, Text, f.opcode)
	assert.Equal(t, []byte("Hello"), f.payload)
}

func TestNewFrameOpcode(t *testing.T) {
	f, err := NewFrameFromBuffer(helloFrame)
	assert.NoError(t, err)
	assert.Equal(t, Text, f.opcode)

	f, err = NewFrameFromBuffer(pingFrame)
	assert.NoError(t, err)
	assert.Equal(t, Ping, f.opcode)

	f, err = NewFrameFromBuffer(pongFrame)
	assert.NoError(t, err)
	assert.Equal(t, Pong, f.opcode)

	f, err = NewFrameFromBuffer(closeFrame)
	assert.NoError(t, err)
	assert.Equal(t, Close, f.opcode)
}

func TestParseFragmentedMessage(t *testing.T) {
	rdr := NewFrameReader(bytes.NewReader(fragmentedMessage))

	f1, err := rdr.Read()
	assert.NoError(t, err)
	require.NotNil(t, f1)
	assert.Equal(t, f1.opcode, Text)
	assert.False(t, f1.fin)

	fp, err := rdr.Read()
	assert.NoError(t, err)
	require.NotNil(t, fp)
	assert.Equal(t, fp.opcode, Ping)
	assert.True(t, fp.fin)

	f2, err := rdr.Read()
	assert.NoError(t, err)
	require.NotNil(t, f2)
	assert.Equal(t, f2.opcode, Continuation)
	assert.False(t, f1.fin)

	fo, err := rdr.Read()
	assert.NoError(t, err)
	require.NotNil(t, fo)
	assert.Equal(t, fo.opcode, Pong)
	assert.True(t, fo.fin)

	f3, err := rdr.Read()
	assert.NoError(t, err)
	require.NotNil(t, f3)
	assert.Equal(t, f3.opcode, Continuation)
	assert.True(t, f3.fin)

	assert.Equal(t, "Hello!", string(f1.payload)+string(f2.payload)+string(f3.payload))

	_, err = rdr.Read()
	assert.Error(t, err)

}

func TestCloseFrame(t *testing.T) {
	f, err := NewFrameFromBuffer(closeFrame)
	assert.NoError(t, err)
	assert.Equal(t, Close, f.opcode)
	assert.Equal(t, uint16(1001), f.closeCode())
}
