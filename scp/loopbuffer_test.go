package scp

import (
	"bytes"
	crand "crypto/rand"
	mrand "math/rand"
	"testing"
)

func TestLoopbuffer(t *testing.T) {
	cap := 10
	lb := newLoopBuffer(cap)
	if lb.Cap() != cap {
		t.Errorf("Cap")
	}

	for i := 0; i < 1000; i++ {
		n := mrand.Intn(2 * cap)
		buf := make([]byte, n)
		crand.Read(buf)
		if wn, err := lb.Write(buf); wn != n || err != nil {
			t.Errorf("Write")
		}

		last := n
		if n > cap {
			last = cap
		}

		if lastBytes, err := lb.ReadLastBytes(last); err != nil {
			t.Errorf("ReadLastBytes: %s", err.Error())
		} else if !bytes.Equal(lastBytes, buf[n-last:]) {
			t.Errorf("ReadLastBytes Unequal, n:%d, last:%d get:% x, expected:% x ", n, last, lastBytes, buf[n-last:])
		}
	}
}
