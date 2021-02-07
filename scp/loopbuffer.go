package scp

import (
	"io"
	"sync"
)

// LoopBuffer .
type LoopBuffer struct {
	buf    []byte // contents are the bytes buf[:off] if not looped or buf[off : cap(buf)] + buff[:off]
	off    int    // write at &buf[off]
	looped bool   // if the buffer is looped
}

// Len returns the number of bytes of the contents of the buffer;
func (b *LoopBuffer) Len() int {
	if b.looped {
		return cap(b.buf)
	}
	return b.off
}

// Cap returns the capacity of the buffer's underlying byte slice, that is, the
// total space allocated for the buffer's data.
func (b *LoopBuffer) Cap() int { return cap(b.buf) }

// Write .
func (b *LoopBuffer) Write(p []byte) (n int, err error) {
	n = len(p)
	capacity := cap(b.buf)

	if n >= capacity {
		copy(b.buf, p[n-capacity:])
		b.looped = true
		b.off = 0
		return
	}

	right := capacity - b.off
	if n < right {
		copy(b.buf[b.off:], p)
		b.off += n
		return
	}

	// fill right
	copy(b.buf[b.off:], p[:right])
	copy(b.buf[0:], p[right:])
	b.looped = true
	b.off = n - right
	return
}

// ReadLastBytes .
func (b *LoopBuffer) ReadLastBytes(n int) (buf []byte, err error) {
	if n > b.Len() {
		err = io.ErrShortBuffer
		return
	}

	buf = make([]byte, n)
	if n <= b.off {
		copy(buf, b.buf[b.off-n:b.off])
		return
	}

	wrapped := n - b.off
	copy(buf, b.buf[cap(b.buf)-wrapped:])
	copy(buf[wrapped:], b.buf[:b.off])
	return
}

// Reset .
func (b *LoopBuffer) Reset() {
	b.off = 0
	b.looped = false
}

// CopyTo .
func (b *LoopBuffer) CopyTo(dst *LoopBuffer) {
	if cap(dst.buf) != cap(b.buf) {
		dst.buf = make([]byte, cap(b.buf))
	}

	copy(dst.buf, b.buf)
	dst.off = b.off
	dst.looped = b.looped
	return
}

// NewLoopBuffer .
func NewLoopBuffer(cap int) *LoopBuffer {
	return &LoopBuffer{
		buf: make([]byte, cap),
	}
}

// LoopBufferPool .
type LoopBufferPool struct {
	Pool sync.Pool
}

// Get .
func (p *LoopBufferPool) Get() *LoopBuffer {
	b := p.Pool.Get().(*LoopBuffer)
	b.Reset()
	return b
}

// Put .
func (p *LoopBufferPool) Put(v *LoopBuffer) {
	p.Pool.Put(v)
}
