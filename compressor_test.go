package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Testing example from rfc:
// https://datatracker.ietf.org/doc/html/rfc7692#section-7.2.3.1
func TestCompressDecompress(t *testing.T) {
	plain := []byte("Hello")

	c := compressors.Get().(*Compressor)
	defer compressors.Put(c)

	data, err := c.compress(plain)
	assert.NoError(t, err)
	assert.Equal(t, []byte{0xf2, 0x48, 0xcd, 0xc9, 0xc9, 0x07, 0x00}, data)

	d := decompressors.Get().(*Decompressor)
	defer decompressors.Put(d)
	plain2, err := d.decompress(data)
	assert.NoError(t, err)
	assert.Equal(t, plain, plain2)
}
