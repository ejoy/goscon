package alg

import (
	"crypto/md5"
	"encoding/binary"
	"io"
)

// little-endian uint64
type Leu64 [8]byte

func ToLeu64(v uint64) Leu64 {
	var l Leu64
	l.PutUint64(v)
	return l
}

func (l Leu64) Uint64() uint64 {
	return binary.LittleEndian.Uint64(l[:])
}

func (l *Leu64) PutUint64(v uint64) {
	binary.LittleEndian.PutUint64(l[:], v)
}

func (l *Leu64) PutLowUint32(v uint32) {
	binary.LittleEndian.PutUint32(l[:4], v)
}

func (l *Leu64) PutHighUint32(v uint32) {
	binary.LittleEndian.PutUint32(l[4:], v)
}

func (l Leu64) Read(p []byte) (int, error) {
	if len(p) < 8 {
		return 0, io.ErrShortBuffer
	}
	return copy(p, l[:]), nil
}

func (l *Leu64) Write(p []byte) (int, error) {
	return copy(l[:], p), nil
}

func Hash(s []byte) Leu64 {
	var djb_hash uint32 = 5381
	var js_hash uint32 = 1315423911

	for _, c := range s {
		djb_hash += (djb_hash << 5) + uint32(c)
		js_hash ^= ((js_hash << 5) + uint32(c) + (js_hash >> 2))
	}

	var v Leu64
	v.PutLowUint32(djb_hash)
	v.PutHighUint32(js_hash)
	return v
}

func Hmac(x, y Leu64) Leu64 {
	var w [48]byte
	x.Read(w[:8])
	y.Read(w[8:])
	copy(w[16:32], w[:16])
	copy(w[32:], w[:16])

	sum := md5.Sum(w[:])

	var a, b Leu64
	a.Write(sum[:8])
	b.Write(sum[8:])

	return ToLeu64(a.Uint64() ^ b.Uint64())
}
