package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeflate(t *testing.T) {
	plain := []byte("Hello")
	data, err := compress(plain)
	assert.NoError(t, err)
	assert.Equal(t, []byte{0xf2, 0x48, 0xcd, 0xc9, 0xc9, 0x07, 0x00}, data)

	plain2, err := decompress(data)
	assert.NoError(t, err)
	assert.Equal(t, plain, plain2)
}
