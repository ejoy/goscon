package scp

import (
	"io"
)

type loopBuffer struct {
	buf    []byte // contents are the bytes buf[:off] if not looped or buf[off : cap(buf)] + buff[:off]
	off    int    // write at &buf[off]
	looped bool   // if the buffer is looped
}

// Len returns the number of bytes of the contents of the buffer;
func (b *loopBuffer) Len() int {
	if b.looped {
		return cap(b.buf)
	}
	return b.off
}

// Cap returns the capacity of the buffer's underlying byte slice, that is, the
// total space allocated for the buffer's data.
func (b *loopBuffer) Cap() int { return cap(b.buf) }

func (b *loopBuffer) Write(p []byte) (n int, err error) {
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

func (b *loopBuffer) ReadLastBytes(n int) (buf []byte, err error) {
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

func deepCopyLoopBuffer(lp *loopBuffer) *loopBuffer {
	buf := make([]byte, cap(lp.buf))
	copy(buf, lp.buf)
	return &loopBuffer{
		buf:    buf,
		off:    lp.off,
		looped: lp.looped,
	}
}

func newLoopBuffer(cap int) *loopBuffer {
	return &loopBuffer{
		buf: make([]byte, cap),
	}
}
