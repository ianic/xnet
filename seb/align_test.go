package seb

import (
	"encoding/binary"
	"testing"
	"unsafe"
)

const cnt = 1024 * 16
const off = 7

func BenchmarkAligned(b *testing.B) {
	var buf [cnt * 8]byte
	for i := 0; i < cnt; i++ {
		offset := i * 8
		binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i))
	}

	for n := 0; n < b.N; n++ {
		for i := 0; i < cnt; i++ {
			offset := i * 8
			ei := binary.LittleEndian.Uint64(buf[offset : offset+8])
			if ei != uint64(i) {
				b.Fail()
			}
		}
	}
}

func BenchmarkUnaligned(b *testing.B) {
	var buf [cnt*8 + off]byte
	for i := 0; i < cnt; i++ {
		offset := i*8 + off
		binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i))
	}

	for n := 0; n < b.N; n++ {
		for i := 0; i < cnt; i++ {
			offset := i*8 + off
			ei := binary.LittleEndian.Uint64(buf[offset : offset+8])
			if ei != uint64(i) {
				b.Fail()
			}
		}
	}
}

func BenchmarkPointer(b *testing.B) {
	var buf [cnt * 8]byte
	for i := 0; i < cnt; i++ {
		offset := i * 8
		binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i))
	}

	for n := 0; n < b.N; n++ {
		for i := 0; i < cnt; i++ {
			offset := i * 8
			ei := *((*uint64)(unsafe.Pointer(&buf[offset])))
			if ei != uint64(i) {
				b.Fail()
			}
		}
	}
}

func BenchmarkUnalignedPointer(b *testing.B) {
	var buf [cnt*8 + off]byte
	for i := 0; i < cnt; i++ {
		offset := i*8 + off
		binary.LittleEndian.PutUint64(buf[offset:offset+8], uint64(i))

	}

	for n := 0; n < b.N; n++ {
		for i := 0; i < cnt; i++ {
			offset := i*8 + off
			// var eip *uint64
			// eip = (*uint64)(unsafe.Pointer(&buf[offset]))
			// if *eip != uint64(i) {
			// 	b.Fail()
			// }
			ei := *((*uint64)(unsafe.Pointer(&buf[offset])))

			if ei != uint64(i) {
				b.Fail()
			}
			//fmt.Printf("%x %x %x\n", &buf[offset], &ei, eip)
		}
	}
}

var sink uint64

func BenchmarkAlignedLoad(b *testing.B) {
	var buf [16]byte
	p := unsafe.Pointer(&buf[0])
	var s uint64
	for i := 0; i < b.N; i++ {
		s += readUnaligned64(p)
	}
	sink = s
}

func BenchmarkUnalignedLoad(b *testing.B) {
	var buf [16]byte
	p := unsafe.Pointer(&buf[1])
	var s uint64
	for i := 0; i < b.N; i++ {
		s += readUnaligned64(p)
	}
	sink = s
}

func readUnaligned64(p unsafe.Pointer) uint64 {
	q := (*[8]byte)(p)
	// if goarch.BigEndian {
	// 	return uint64(q[7]) | uint64(q[6])<<8 | uint64(q[5])<<16 | uint64(q[4])<<24 |
	// 		uint64(q[3])<<32 | uint64(q[2])<<40 | uint64(q[1])<<48 | uint64(q[0])<<56
	// }
	return uint64(q[0]) | uint64(q[1])<<8 | uint64(q[2])<<16 | uint64(q[3])<<24 | uint64(q[4])<<32 | uint64(q[5])<<40 | uint64(q[6])<<48 | uint64(q[7])<<56
}
