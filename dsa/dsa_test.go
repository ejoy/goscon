package dsa

import (
	"testing"
)

const privatePEM = `
-----BEGIN DSA PRIVATE KEY-----
MIIBuwIBAAKBgQCxxFpIwq3oF0945+/pMb0zZgIIVZn8zu/xTmkaXtAaJiaRFT1D
wJyTHjTBahGHDi0t8V0iYBtSkEiePU7h7CBZChL2hlM3ctC0thJ+RJHT2+oRHqpO
CPpCw20zGfhAywhJgP1NWC68GeyWkuH6ujgrjZC7xNrAZaEWiCs7ewzZbQIVAPMC
mVYqD/WiqfWCe117y/eQNyuXAoGAW9lWkkMen2bBhtD0Gw4GCrJUJolFpCy8T4vX
rrrRgUzTeKRQKetsSeHj+HMcp7QSg2zZzypQ/X8bySjaVvmHGdZ5QNcwpE6Wq+XT
KKeXtEceRFRWcB8SejH6WJx6AtGCpAnqNZhQBChFpWPzuX5qLN8IoztyUMjX4fEv
cBMd7l0CgYBCvlxemJXVXSLdawcxu7DEyRAJE2khLtGsCTZiMdufAFpdUMf0S5U8
7Q8tVFywL8qWurFkO92OxuAMwahT2vSLq0oy9jH5Lfsn7GyF8Kh7ymgsMVuzvYJI
NTRraScNo0YJplJja8CvMf0KQenIteHtEZN0U7/14Jf3cg5cC4yP/wIVAN4ZO0uc
4+euQ1oBFdeBwng6jCxN
-----END DSA PRIVATE KEY-----
`

const pubPEM = `
-----BEGIN PUBLIC KEY-----
MIIBtjCCASsGByqGSM44BAEwggEeAoGBALHEWkjCregXT3jn7+kxvTNmAghVmfzO
7/FOaRpe0BomJpEVPUPAnJMeNMFqEYcOLS3xXSJgG1KQSJ49TuHsIFkKEvaGUzdy
0LS2En5EkdPb6hEeqk4I+kLDbTMZ+EDLCEmA/U1YLrwZ7JaS4fq6OCuNkLvE2sBl
oRaIKzt7DNltAhUA8wKZVioP9aKp9YJ7XXvL95A3K5cCgYBb2VaSQx6fZsGG0PQb
DgYKslQmiUWkLLxPi9euutGBTNN4pFAp62xJ4eP4cxyntBKDbNnPKlD9fxvJKNpW
+YcZ1nlA1zCkTpar5dMop5e0Rx5EVFZwHxJ6MfpYnHoC0YKkCeo1mFAEKEWlY/O5
fmos3wijO3JQyNfh8S9wEx3uXQOBhAACgYBCvlxemJXVXSLdawcxu7DEyRAJE2kh
LtGsCTZiMdufAFpdUMf0S5U87Q8tVFywL8qWurFkO92OxuAMwahT2vSLq0oy9jH5
Lfsn7GyF8Kh7ymgsMVuzvYJINTRraScNo0YJplJja8CvMf0KQenIteHtEZN0U7/1
4Jf3cg5cC4yP/w==
-----END PUBLIC KEY-----
`

func TestDSA(t *testing.T) {
	priv, err := ParseDSAPrivateKeyFromPEM([]byte(privatePEM))
	if err != nil {
		t.Fatalf("ParseDSAPrivateKeyFromPEM failed: %s", err)
	}
	pub, err := ParseDSAPublicKeyFromPEM([]byte(pubPEM))
	if err != nil {
		t.Fatalf("ParseDSAPrivateKeyFromPEM failed: %s", err)
	}

	content := []byte("hello world")
	r, s, err := Sign(priv, content)
	if err != nil {
		t.Fatalf("Sign failed: %s", err)
	}

	if !Verify(pub, content, r, s) {
		t.Fatalf("Verify failed: %s", err)
	}
}
