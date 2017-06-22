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
	"net"
	"strconv"
	"strings"

	"github.com/ejoy/goscon/dh64"
)

type NewConnReq struct {
	id  uint32
	key leu64
}

// code:
//  200 OK
//  401 Unauthorized
//  403 Index Expired
//  404 Index Not Found
//  406 Not Acceptable

type ReuseConnReq struct {
	id       uint32
	index    uint32
	received uint32
	token    leu64
}

func b64decodeUint64(src []byte) (v leu64, err error) {
	n := base64.StdEncoding.DecodedLen(len(src))
	if n != 8 {
		err = errors.New("wrong dh key length")
		return
	}

	dst := make([]byte, n)
	if _, err = base64.StdEncoding.Decode(dst, src); err != nil {
		Error("decoding base64 dh_key failed:%v", err)
		return
	}

	v.Write(dst)
	return
}

func b64encodeUint64(v leu64) string {
	return base64.StdEncoding.EncodeToString(v[:])
}

func parseNewConnReq(id uint32, slots [][]byte) (err error, req *NewConnReq) {
	if len(slots) < 1 {
		Debug("parse new conn request failed")
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
		Debug("parse reuse conn request failed")
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
		return
	}

	buf := make([]byte, sz)
	c := 0
	for c < int(sz) {
		var n int
		n, err = conn.Read(buf[c:])
		if err != nil {
			return
		}
		c += n
	}

	Debug("conn: %v, recv req: %s", conn.RemoteAddr(), string(buf))
	slots := bytes.Split(buf, []byte("\n"))
	if len(slots) < 1 {
		Debug("conn: %v, parse req failed:%s", conn.RemoteAddr(), string(buf))
		err = errors.New("wrong request")
		return
	}
	id, err := strconv.Atoi(string(slots[0]))
	if err != nil {
		Debug("conn: %v, parse req failed:%s", conn.RemoteAddr(), err.Error())
		return
	}
	if id == 0 {
		err, req = parseNewConnReq(uint32(id), slots[1:])
	} else {
		err, req = parseReuseConnReq(uint32(id), slots[1:])
	}
	return
}

func writeResp(conn *net.TCPConn, slots []string) error {
	chunk := strings.Join(slots, "\n")
	sz := uint16(len(chunk))
	err := binary.Write(conn, binary.BigEndian, sz)
	if err != nil {
		return err
	}

	Debug("send resp: %s", string(chunk))
	_, err = conn.Write([]byte(chunk))
	return err
}

func WriteNewConnResp(conn *net.TCPConn, id uint32, key leu64) error {
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

func GenToken(clientPubKey leu64) (leu64, leu64) {
	priKey := dh64.PrivateKey()
	pubKey := dh64.PublicKey(priKey)
	secret := dh64.Secret(priKey, clientPubKey.Uint64())
	Debug("privateKey:0x%x, publicKey:0x%x, Secret:0x%x\n", priKey, pubKey, secret)
	return toLeu64(pubKey), toLeu64(secret)
}

func VerifySecret(secret leu64, req *ReuseConnReq) bool {
	content := []byte(fmt.Sprintf("%d\n%d\n%d\n", req.id, req.index, req.received))
	x := Hash(content)
	token := Hmac(x, secret)
	Debug("content:%s, hashkey:%x, secret:%x, token:%x, req.token:%x", string(content), x.Uint64(), secret, token, req.token)
	return token == req.token
}

func GenRC4Key(v1 leu64, v2 leu64, key []byte) {
	h := Hmac(v1, v2)
	copy(key, h[:])
}
