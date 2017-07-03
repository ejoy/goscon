package dh64

import (
	"math/rand"
)

const (
	p uint64 = 0xffffffffffffffc5
	g uint64 = 5
)

func mulModP(a, b uint64) uint64 {
	var m uint64
	for b > 0 {
		if b&1 > 0 {
			t := p - a
			if m >= t {
				m -= t
			} else {
				m += a
			}
		}
		if a >= p-a {
			a = a*2 - p
		} else {
			a = a * 2
		}
		b >>= 1
	}
	return m
}

func powModP(a, b uint64) uint64 {
	if b == 1 {
		return a
	}
	t := powModP(a, b>>1)
	t = mulModP(t, t)
	if b%2 > 0 {
		t = mulModP(t, a)
	}
	return t
}

func powmodp(a uint64, b uint64) uint64 {
	if a == 0 {
		panic("DH64 zero public key")
	}
	if b == 0 {
		panic("DH64 zero private key")
	}
	if a > p {
		a %= p
	}
	return powModP(a, b)
}

// KeyPair returns a pair of key
func PrivateKey() uint64 {
	return rand.Uint64()
}

// PublicKey returns the public key corresponding to the privateKey
func PublicKey(privateKey uint64) uint64 {
	return powmodp(g, privateKey)
}

// Secret returns the secret
func Secret(privateKey, anotherPublicKey uint64) uint64 {
	return powmodp(anotherPublicKey, privateKey)
}
