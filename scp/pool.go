package scp

import (
	"bytes"
	"sync"
)

type bufferPool struct {
	pool sync.Pool
}

// Get gets a buffer, and guarantees cap for n bytes
func (p *bufferPool) Get(n int) *bytes.Buffer {
	b := p.pool.Get().(*bytes.Buffer)
	b.Reset()
	b.Grow(n)
	return b
}

func (p *bufferPool) Put(b *bytes.Buffer) {
	p.pool.Put(b)
}

func newBufferPool() *bufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

var defaultBufferPool *bufferPool

func init() {
	defaultBufferPool = newBufferPool()
}
