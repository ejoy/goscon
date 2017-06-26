package scp

import (
	"sync"
)

type IDAllocator struct {
	sync.Mutex
	start int
	off   int
	free  []int
}

func (o *IDAllocator) AcquireID() int {
	o.Lock()
	defer o.Unlock()
	if len(o.free) > 0 {
		index := len(o.free) - 1
		id := o.free[index]
		o.free = o.free[:index]
		return id
	}
	id := o.off
	o.off = o.off + 1
	return id
}

func (o *IDAllocator) ReleaseID(id int) {
	o.Lock()
	defer o.Unlock()

	if id > o.off {
		panic("id > o.off")
	}

	index := len(o.free)
	if index == cap(o.free) {
		free := make([]int, index, index*2+1)
		copy(free, o.free)
		o.free = free
	}
	o.free = append(o.free, id)

	// all released
	if len(o.free) == o.off-o.start {
		o.off = o.start
		o.free = o.free[:0]
	}
}

func NewIDAllocator(start int) *IDAllocator {
	if start < 0 {
		panic("start < 0")
	}
	return &IDAllocator{
		start: start,
		off:   start,
	}
}
