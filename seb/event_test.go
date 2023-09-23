package seb

import (
	"encoding/binary"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventEncode(t *testing.T) {
	body, err := randomBuf(128)
	require.NoError(t, err)
	e := Event{
		Sequence:  uint64(0xa1_b2_c3_d4_e5_f6_a7_b8),
		Timestamp: 0x_17_87_42_71_94_93_c4_08,
		Type:      DeltaEvent,
		Body:      body,
	}
	buf := make([]byte, e.FrameLen()-1)
	err = e.Encode(buf)
	require.ErrorIs(t, err, ErrInsufficientBuffer)

	require.Equal(t, e.FrameLen(), 1+8+8+1+4+len(e.Body))
	buf = make([]byte, e.FrameLen())
	err = e.Encode(buf)
	require.NoError(t, err)

	require.Equal(t, byte(OpEvent), buf[0])
	require.Equal(t, e.Sequence, binary.LittleEndian.Uint64(buf[1:9]))
	require.Equal(t, e.Timestamp, binary.LittleEndian.Uint64(buf[9:17]))
	require.Equal(t, byte(e.Type), buf[17])
	require.Equal(t, uint32(len(body)), binary.LittleEndian.Uint32(buf[18:22]))
	require.Equal(t, e.Body, buf[22:])

	var e2 Event
	err = e2.Decode(buf[:len(buf)-1])
	require.ErrorIs(t, err, ErrSplitBuffer)

	err = e2.Decode(buf)
	require.NoError(t, err)
	require.Equal(t, e, e2)
}

func randomBuf(size int) ([]byte, error) {
	f, err := os.Open("/dev/random")
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
