package scp

import (
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
	id  int
	key leu64
}

func (r *newConnReq) marshal() []byte {
	s := fmt.Sprintf("%d\n%s", r.id, b64encodeLeu64(r.key))
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

func (r *reuseConnReq) setSum(secret leu64) {
	s := fmt.Sprintf("%d\n%d\n%d\n", r.id, r.handshakes, r.received)
	r.sum = hmac(hash([]byte(s)), secret)
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
}

func (r *reuseConnResp) marshal() []byte {
	s := fmt.Sprintf("%d\n%d", r.received, r.code)
	return []byte(s)
}

func (r *reuseConnResp) unmarshal(s []byte) (err error) {
	lines := strings.Split(string(s), "\n")
	if len(lines) < 2 {
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
	return nil
}

type serverReq struct {
	req handshakeMessage
}

func (r *serverReq) marshal() []byte {
	return nil
}

func (r *serverReq) unmarshal([]byte) error {
	return nil
}
