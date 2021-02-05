package scp

import (
	"io"
	"sync"
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

// Write .
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

// ReadLastBytes .
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

// Reset .
func (b *loopBuffer) Reset() {
	b.off = 0
	b.looped = false
}

// CopyTo .
func (b *loopBuffer) CopyTo(dst *loopBuffer) {
	if cap(dst.buf) != cap(b.buf) {
		dst.buf = make([]byte, cap(b.buf))
	}

	copy(dst.buf, b.buf)
	dst.off = b.off
	dst.looped = b.looped
	return
}

func newLoopBuffer(cap int) *loopBuffer {
	return &loopBuffer{
		buf: make([]byte, cap),
	}
}

type loopBufferPool struct {
	pool sync.Pool
}

func (p *loopBufferPool) Get() *loopBuffer {
	b := p.pool.Get().(*loopBuffer)
	b.Reset()
	return b
}

// if ReuseBufferSize changes, invalidate all old buffer
func (p *loopBufferPool) Put(v *loopBuffer) {
	p.pool.Put(v)
}

func newLoopBufferPool(size int) *loopBufferPool {
	return &loopBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return newLoopBuffer(size)
			},
		},
	}
}

var loopBufferPoolManager = make(map[int]*loopBufferPool, 2)
var managerMutex sync.Mutex

func getLoopBuffer(size int) *loopBuffer {
	managerMutex.Lock()
	defer managerMutex.Unlock()
	if pool, ok := loopBufferPoolManager[size]; ok {
		return pool.Get()
	}
	loopBufferPoolManager[size] = newLoopBufferPool(size)
	return loopBufferPoolManager[size].Get()
}

func putLoopBuffer(size int, v *loopBuffer) {
	managerMutex.Lock()
	defer managerMutex.Unlock()
	loopBufferPoolManager[size].Put(v)
}
