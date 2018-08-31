package scp

import (
	"testing"
)

func TestIDAllocator(t *testing.T) {
	start := 1
	allocator := NewIDAllocator(start)

	startID := allocator.AcquireID()
	if startID != start {
		t.Errorf("Start ID")
	}

	allocator.ReleaseID(startID)

	nextID := start
	for i := 0; i < 100; i++ {
		id := allocator.AcquireID()
		if id != nextID {
			t.Errorf("Acquire ID: %d:%d", nextID, id)
		}
		nextID = nextID + 1
	}

	for i := start; i < nextID; i++ {
		allocator.ReleaseID(i)
	}

	if allocator.AcquireID() != start {
		t.Errorf("Start ID")
	}
}
