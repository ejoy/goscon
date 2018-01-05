package dsa

import (
	"crypto/dsa"
	"crypto/md5"
	"crypto/rand"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"io/ioutil"
	"math/big"
	"os"
)

func readFile(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	return ioutil.ReadAll(f)
}

// ParseDSAPrivateKey returns a DSA private key from its ASN.1 DER encoding, as
// specified by the OpenSSL DSA man page.
func ParseDSAPrivateKey(der []byte) (*dsa.PrivateKey, error) {
	var k struct {
		Version int
		P       *big.Int
		Q       *big.Int
		G       *big.Int
		Pub     *big.Int
		Priv    *big.Int
	}

	rest, err := asn1.Unmarshal(der, &k)
	if err != nil {
		return nil, errors.New("failed to parse DSA key: " + err.Error())
	}
	if len(rest) > 0 {
		return nil, errors.New("garbage after DSA key")
	}

	return &dsa.PrivateKey{
		PublicKey: dsa.PublicKey{
			Parameters: dsa.Parameters{
				P: k.P,
				Q: k.Q,
				G: k.G,
			},
			Y: k.Pub,
		},
		X: k.Priv,
	}, nil
}

func ParseDSAPrivateKeyFromPEM(data []byte) (*dsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("failed to parse PEM block")
	}
	return ParseDSAPrivateKey(block.Bytes)
}

func ParseDSAPrivateKeyFromFile(path string) (*dsa.PrivateKey, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}

	return ParseDSAPrivateKeyFromPEM(data)
}

func ParseDSAPublicKey(der []byte) (*dsa.PublicKey, error) {
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}

	switch pub := pub.(type) {
	case *dsa.PublicKey:
		return pub, nil
	default:
		return nil, errors.New("invalid type of public key")
	}
}

func ParseDSAPublicKeyFromPEM(data []byte) (*dsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("failed to parse PEM block")
	}
	return ParseDSAPublicKey(block.Bytes)
}

func ParseDSAPublicKeyFromFile(path string) (*dsa.PublicKey, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, err
	}

	return ParseDSAPublicKeyFromPEM(data)
}

func Sign(priv *dsa.PrivateKey, content []byte) (*big.Int, *big.Int, error) {
	sum := md5.Sum(content)
	return dsa.Sign(rand.Reader, priv, sum[:])
}

func Verify(pub *dsa.PublicKey, content []byte, r, s *big.Int) bool {
	sum := md5.Sum(content)
	return dsa.Verify(pub, sum[:], r, s)
}
