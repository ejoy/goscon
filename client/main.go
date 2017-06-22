package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/ejoy/goscon/alg"
	"github.com/ejoy/goscon/dh64"
)

type NewConnReq struct {
	id  uint32
	key alg.Leu64
}

func b64encodeUint64(v alg.Leu64) string {
	return base64.StdEncoding.EncodeToString(v[:])
}

func writeRequest(conn net.Conn, slots []string) error {
	chunk := strings.Join(slots, "\n")
	sz := uint16(len(chunk))
	err := binary.Write(conn, binary.BigEndian, sz)
	if err != nil {
		return err
	}

	_, err = conn.Write([]byte(chunk))
	return err
}

func main() {
	if len(os.Args) <= 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s <address>", os.Args[0])
		return
	}

	addr := os.Args[1]
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connect failed: %s", err.Error())
		return
	}

	req := new(NewConnReq)
	privateKey := dh64.PrivateKey()
	publicKey := dh64.PublicKey(privateKey)
	req.key = alg.ToLeu64(publicKey)

	slots := make([]string, 2)
	slots[0] = strconv.FormatUint(uint64(req.id), 10)
	slots[1] = b64encodeUint64(req.key)

	if err := writeRequest(conn, slots); err != nil {
		fmt.Fprintf(os.Stderr, "Write failed: %s", err.Error())
		return
	}
	io.Copy(conn, os.Stdin)
}
