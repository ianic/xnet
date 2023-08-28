package ws

import (
	"bytes"
	"testing"
)

// Testing example from rfc:
// https://datatracker.ietf.org/doc/html/rfc7692#section-7.2.3.1
func TestCompressDecompress(t *testing.T) {
	plain := []byte("Hello")

	c := compressors.Get().(*Compressor)
	defer compressors.Put(c)

	data, err := c.compress(plain)
	if err != nil {
		t.Fatal(err)
	}
	compressed := []byte{0xf2, 0x48, 0xcd, 0xc9, 0xc9, 0x07, 0x00}
	if !bytes.Equal(data, compressed) {
		t.Fatalf("compressed %x", data)
	}

	d := decompressors.Get().(*Decompressor)
	defer decompressors.Put(d)
	data, err = d.decompress(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, plain) {
		t.Fatalf("plain %x", data)
	}
}
