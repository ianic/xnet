package ws

import (
	"bytes"
	"io"
	"sync"

	"github.com/klauspost/compress/flate"
)

var compressors = sync.Pool{
	New: func() any {
		return NewCompressor()
	},
}

var decompressors = sync.Pool{
	New: func() any {
		return NewDecompressor()
	},
}

func Decompress(payload []byte) ([]byte, error) {
	dc := decompressors.Get().(*Decompressor)
	defer decompressors.Put(dc)
	return dc.decompress(payload)
}

func Compress(payload []byte) ([]byte, error) {
	cp := compressors.Get().(*Compressor)
	defer compressors.Put(cp)
	return cp.compress(payload)
}

type Compressor struct {
	w *flate.Writer
}

func NewCompressor() *Compressor {
	w, _ := flate.NewWriter(nil, 7)
	return &Compressor{
		w: w,
	}
}

func (c *Compressor) compress(payload []byte) ([]byte, error) {
	buf := &bytes.Buffer{}
	w := c.w
	w.Reset(buf)

	if _, err := w.Write(payload); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}

	b := buf.Bytes()
	return b[:len(b)-4], nil
}

type Decompressor struct {
	r io.ReadCloser
}

func NewDecompressor() *Decompressor {
	return &Decompressor{
		r: flate.NewReader(nil),
	}
}

var compressLastBlock = []byte{0x00, 0x00, 0xff, 0xff, 0x01, 0x00, 0x00, 0xff, 0xff}

func (c *Decompressor) decompress(payload []byte) ([]byte, error) {
	rd := bytes.NewReader(append(payload, compressLastBlock...))
	c.r.(flate.Resetter).Reset(rd, nil)
	return io.ReadAll(c.r)
}
