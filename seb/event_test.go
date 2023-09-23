package seb

import (
	"encoding/binary"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventEncode(t *testing.T) {
	e := Event{
		Sequence:  uint64(0xa1_b2_c3_d4_e5_f6_a7_b8),
		Timestamp: 0x_17_87_42_71_94_93_c4_08,
		Type:      DeltaEvent,
		Body:      randomBuf(t, 128),
	}
	buf := make([]byte, e.FrameLen()-1)
	err := e.Encode(buf)
	require.ErrorIs(t, err, ErrInsufficientBuffer)

	require.Equal(t, e.FrameLen(), 1+8+8+1+4+len(e.Body))
	buf = make([]byte, e.FrameLen())
	err = e.Encode(buf)
	require.NoError(t, err)

	require.Equal(t, byte(OpEvent), buf[0])
	require.Equal(t, e.Sequence, binary.LittleEndian.Uint64(buf[1:9]))
	require.Equal(t, e.Timestamp, binary.LittleEndian.Uint64(buf[9:17]))
	require.Equal(t, byte(e.Type), buf[17])
	require.Equal(t, uint32(128), binary.LittleEndian.Uint32(buf[18:22]))
	require.Equal(t, e.Body, buf[22:])

	var e2 Event
	err = e2.Decode(buf[:len(buf)-1])
	require.ErrorIs(t, err, ErrSplitBuffer)

	err = e2.Decode(buf)
	require.NoError(t, err)
	require.Equal(t, e, e2)
}

func TestWriterHeadTail(t *testing.T) {
	buf := make([]byte, 100)
	w := &writer{buf: buf}
	e1 := Event{
		Sequence:  1,
		Timestamp: 2,
		Type:      StateEvent,
		Body:      make([]byte, 100),
	}

	// body does not fit
	require.Equal(t, 0, w.head)
	require.Equal(t, 0, w.tail)
	err := e1.encode(w)
	require.ErrorIs(t, err, ErrInsufficientBuffer)
	require.Equal(t, 0, w.head)
	require.Equal(t, 22, w.tail)
	w.reset()
	require.Equal(t, 0, w.tail)

	// make smaller body so it fits into buffer few times
	e1.Body = make([]byte, 16)
	err = e1.encode(w)
	require.NoError(t, err)
	require.Equal(t, 38, w.head)
	require.Equal(t, 38, w.tail)

	err = e1.encode(w)
	require.NoError(t, err)
	require.Equal(t, 76, w.head)
	require.Equal(t, 76, w.tail)

	// tail is moved head is where last successful write finished
	err = e1.encode(w)
	require.ErrorIs(t, err, ErrInsufficientBuffer)
	require.Equal(t, 76, w.head)
	require.Equal(t, 98, w.tail)
}

func randomBuf(t *testing.T, size int) []byte {
	f, err := os.Open("/dev/random")
	require.NoError(t, err)
	buf := make([]byte, size)
	_, err = io.ReadFull(f, buf)
	require.NoError(t, err)
	return buf
}
