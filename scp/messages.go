package scp

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

func b64decodeLeu64(src string) (v leu64, err error) {
	n := base64.StdEncoding.DecodedLen(len(src))
	if n < 8 {
		err = ErrIllegalMsg
		return
	}

	dst := make([]byte, n)
	if _, err = base64.StdEncoding.Decode(dst, []byte(src)); err != nil {
		return
	}

	v.Write(dst)
	return
}

func b64encodeLeu64(v leu64) string {
	return base64.StdEncoding.EncodeToString(v[:])
}

type handshakeMessage interface {
	marshal() []byte
	unmarshal([]byte) error
}

type newConnReq struct {
	id           int
	key          leu64
	targetServer string
}

func (r *newConnReq) marshal() []byte {
	s := fmt.Sprintf("%d\n%s", r.id, b64encodeLeu64(r.key))
	if r.targetServer != "" {
		s += fmt.Sprintf("\n%s", r.targetServer)
	}
	return []byte(s)
}

func (r *newConnReq) unmarshal(s []byte) (err error) {
	lines := strings.Split(string(s), "\n")
	if len(lines) < 2 {
		err = ErrIllegalMsg
		return
	}

	if r.id, err = strconv.Atoi(lines[0]); err != nil {
		return
	}

	if r.key, err = b64decodeLeu64(lines[1]); err != nil {
		return
	}

	if len(lines) >= 3 {
		r.targetServer = lines[2]
	}
	return
}

type newConnResp struct {
	id  int
	key leu64
}

func (r *newConnResp) marshal() []byte {
	s := fmt.Sprintf("%d\n%s", r.id, b64encodeLeu64(r.key))
	return []byte(s)
}

func (r *newConnResp) unmarshal(s []byte) (err error) {
	lines := strings.Split(string(s), "\n")
	if len(lines) < 2 {
		err = ErrIllegalMsg
		return
	}

	if r.id, err = strconv.Atoi(lines[0]); err != nil {
		return
	}

	if r.key, err = b64decodeLeu64(lines[1]); err != nil {
		return
	}
	return
}

type reuseConnReq struct {
	id         int
	handshakes int // reuse times
	received   uint32
	sum        leu64 // checksum
}

func (r *reuseConnReq) calcSum(secret leu64) leu64 {
	s := fmt.Sprintf("%d\n%d\n%d\n", r.id, r.handshakes, r.received)
	return hmac(hash([]byte(s)), secret)
}

func (r *reuseConnReq) verifySum(secret leu64) bool {
	sum := r.calcSum(secret)
	return bytes.Equal(r.sum[:], sum[:])
}

func (r *reuseConnReq) fillSum(secret leu64) {
	r.sum = r.calcSum(secret)
}

func (r *reuseConnReq) marshal() []byte {
	s := fmt.Sprintf("%d\n%d\n%d\n%s", r.id, r.handshakes, r.received, b64encodeLeu64(r.sum))
	return []byte(s)
}

func (r *reuseConnReq) unmarshal(s []byte) (err error) {
	lines := strings.Split(string(s), "\n")
	if len(lines) < 4 {
		err = ErrIllegalMsg
		return
	}

	if r.id, err = strconv.Atoi(lines[0]); err != nil {
		return
	}

	if r.handshakes, err = strconv.Atoi(lines[1]); err != nil {
		return
	}

	var received uint64
	if received, err = strconv.ParseUint(string(lines[2]), 10, 32); err != nil {
		return
	}
	r.received = uint32(received)

	if r.sum, err = b64decodeLeu64(lines[3]); err != nil {
		return
	}

	return nil
}

type reuseConnResp struct {
	received uint32
	code     int
	sum      leu64 // checksum
}

func (r *reuseConnResp) calcSum(secret leu64) leu64 {
	s := fmt.Sprintf("%d\n%d\n", r.received, r.code)
	return hmac(hash([]byte(s)), secret)
}

func (r *reuseConnResp) verifySum(secret leu64) bool {
	sum := r.calcSum(secret)
	return bytes.Equal(r.sum[:], sum[:])
}

func (r *reuseConnResp) fillSum(secret leu64) {
	r.sum = r.calcSum(secret)
}

func (r *reuseConnResp) marshal() []byte {
	s := fmt.Sprintf("%d\n%d\n%s", r.received, r.code, b64encodeLeu64(r.sum))
	return []byte(s)
}

func (r *reuseConnResp) unmarshal(s []byte) (err error) {
	lines := strings.Split(string(s), "\n")
	if len(lines) < 3 {
		err = ErrIllegalMsg
		return
	}

	var received uint64
	if received, err = strconv.ParseUint(string(lines[0]), 10, 32); err != nil {
		return
	}
	r.received = uint32(received)

	if r.code, err = strconv.Atoi(lines[1]); err != nil {
		return
	}

	if r.sum, err = b64decodeLeu64(lines[2]); err != nil {
		return
	}
	return nil
}

type serverReq struct {
	msg handshakeMessage
}

func (r *serverReq) marshal() []byte {
	panic("serverReq marshal")
}

func (r *serverReq) unmarshal(s []byte) error {
	if strings.HasPrefix(string(s), "0\n") {
		var nq newConnReq
		if err := nq.unmarshal(s); err != nil {
			return err
		}
		r.msg = &nq
	} else {
		var rq reuseConnReq
		if err := rq.unmarshal(s); err != nil {
			return err
		}
		r.msg = &rq
	}
	return nil
}
