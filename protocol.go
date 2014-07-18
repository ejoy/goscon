//
//   date  : 2014-05-23 17:35
//   author: xjdrew
//

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"unsafe"
)

// #include <stdlib.h>
// #include <stdint.h>
// #include "encrypt.h"
import "C"

type NewConnReq struct {
	id  uint32
	key uint64
}

type NewConnResp struct {
	id  uint32
	key uint64
}

type ReuseConnReq struct {
	id       uint32
	index    uint32
	received uint32
	token    uint64
}

// code:
//  200 OK
//  401 Unauthorized
//  403 Index Expired
//  404 Index Not Found
//  406 Not Acceptable
//  501 Not Implemented
type ReuseConnResp struct {
	received uint64
	code     uint32
}

func b64decodeUint64(src []byte) (key uint64, err error) {
	dst := make([]byte, base64.StdEncoding.DecodedLen(len(src)))
	n, err := base64.StdEncoding.Decode(dst, src)
	if err != nil {
		fmt.Printf("decoding base64 dh_key failed:%v", err)
		return
	}
	if n < 8 {
		err = errors.New("wrong dh key length")
		return
	}

	key = uint64(C.uint64_decode((*C.uint8_t)(unsafe.Pointer(&dst[0])), C.int(n)))
	return
}

func b64encodeUint64(val uint64) string {
	buf := make([]byte, 8)
	C.uint64_encode(C.uint64_t(val), (*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(8))
	return base64.StdEncoding.EncodeToString(buf)
}

func parseNewConnReq(id uint32, slots [][]byte) (err error, req *NewConnReq) {
	if len(slots) < 1 {
		fmt.Printf("parse new conn request failed")
		err = errors.New("wrong new conn request")
		return
	}
	key, err := b64decodeUint64(slots[0])
	if err != nil {
		return
	}

	req = new(NewConnReq)
	req.id = id
	req.key = key
	return
}

func parseReuseConnReq(id uint32, slots [][]byte) (err error, req *ReuseConnReq) {
	if len(slots) < 3 {
		fmt.Printf("parse reuse conn request failed")
		err = errors.New("wrong reuse conn request")
		return
	}
	index, err := strconv.ParseUint(string(slots[0]), 10, 32)
	if err != nil {
		return
	}

	received, err := strconv.ParseUint(string(slots[1]), 10, 32)
	if err != nil {
		return
	}

	key, err := b64decodeUint64(slots[2])
	if err != nil {
		return
	}
	req = new(ReuseConnReq)
	req.id = id
	req.index = uint32(index)
	req.received = uint32(received)
	req.token = key
	return
}

func ReadReq(conn *net.TCPConn) (err error, req interface{}) {
	var sz uint16
	err = binary.Read(conn, binary.BigEndian, &sz)
	if err != nil {
		fmt.Printf("read request header failed: %v", err)
		return
	}

	buf := make([]byte, sz)
	c := 0
	for c < int(sz) {
		var n int
		n, err = conn.Read(buf[c:])
		if err != nil {
			fmt.Printf("read request body failed: %v", err)
			return
		}
		c += n
	}

	fmt.Printf("recv req: %s\n", string(buf))
	slots := bytes.Split(buf, []byte("\n"))
	if len(slots) < 1 {
		fmt.Printf("parse request failed")
		err = errors.New("wrong request")
		return
	}
	id, err := strconv.Atoi(string(slots[0]))
	if err != nil {
		fmt.Printf("parse conn id failed:%v", err)
		return
	}
	if id == 0 {
		err, req = parseNewConnReq(uint32(id), slots[1:])
	} else {
		err, req = parseReuseConnReq(uint32(id), slots[1:])
	}
	return
}

func WriteAll(w io.Writer, data []byte) error {
	fmt.Printf("------- write: \n%s\n-------\n", string(data))
	c := 0
	for c < len(data) {
		i, err := w.Write(data[c:])
		if err != nil {
			return err
		}
		c += i
	}
	return nil
}

func writeResp(conn *net.TCPConn, slots []string) error {
	chunk := strings.Join(slots, "\n")
	sz := uint16(len(chunk))
	err := binary.Write(conn, binary.BigEndian, sz)
	if err != nil {
		return err
	}

	fmt.Printf("send resp: %s\n", string(chunk))
	return WriteAll(conn, []byte(chunk))
}

func WriteNewConnResp(conn *net.TCPConn, id uint32, key uint64) error {
	slots := make([]string, 2)
	slots[0] = strconv.FormatUint(uint64(id), 10)
	slots[1] = b64encodeUint64(key)
	return writeResp(conn, slots)
}

func WriteReuseConnResp(conn *net.TCPConn, received uint32, code uint32) error {
	slots := make([]string, 2)
	slots[0] = strconv.FormatUint(uint64(received), 10)
	slots[1] = strconv.FormatUint(uint64(code), 10)
	return writeResp(conn, slots)
}

func Gentoken(key uint64) (token uint64, secret uint64) {
	random := uint64(C.randomint64())
	token = uint64(C.exchange(C.uint64_t(C.uint64_t(random))))
	secret = uint64(C.secret(C.uint64_t(key), C.uint64_t(random)))
	fmt.Printf("random:%x, token:%x, secret:%x\n", random, token, secret)
	return
}

func VerifySecret(req *ReuseConnReq, link *StableLink) bool {
	content := []byte(fmt.Sprintf("%d\n%d\n%d\n", req.id, req.index, req.received))
	x := C.hash((*C.uint8_t)(&content[0]), C.int(len(content)))
	token := uint64(C.hmac(x, C.uint64_t(link.secret)))
	fmt.Printf("content:%s\n", string(content))
	fmt.Printf("hashkey:%x\n", uint64(x))
	fmt.Printf("secret:%x\n", link.secret)
	fmt.Printf("token:%x\n", token)
	fmt.Printf("req.token:%x\n", req.token)
	return token == req.token
}
