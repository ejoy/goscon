package dh64

import (
	"math/big"
	"math/rand"
	"testing"
)

func Test_MulModP(t *testing.T) {
	for i := 0; i < 10000; i++ {
		a := rand.Uint32()
		b := rand.Uint32()
		x := new(big.Int).Mod(
			new(big.Int).Mul(
				big.NewInt(int64(a)),
				big.NewInt(int64(b)),
			),
			new(big.Int).SetUint64(p),
		).Uint64()

		m := mulModP(uint64(a), uint64(b))
		if x != m {
			t.Fail()
		}
	}
}

func Test_DH64_GO(t *testing.T) {
	for i := 0; i < 10000; i += 2 {
		privateKey1 := PrivateKey()
		publicKey1 := PublicKey(privateKey1)

		privateKey2 := PrivateKey()
		publicKey2 := PublicKey(privateKey2)

		secret1 := Secret(privateKey1, publicKey2)
		secret2 := Secret(privateKey2, publicKey1)
		if secret1 != secret2 {
			t.Fail()
		}
	}
}
