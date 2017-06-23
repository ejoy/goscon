package main

import (
	"fmt"
	"io"
	"net"
	"os"

	"github.com/ejoy/goscon/scp"
)

func main() {
	if len(os.Args) <= 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s <address>", os.Args[0])
		return
	}

	addr := os.Args[1]
	old, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connect failed: %s", err.Error())
		return
	}

	oldScon := scp.Client(old, nil)

	if _, err := oldScon.Write([]byte("hello\n")); err != nil {
		fmt.Fprintf(os.Stderr, "Write failed: %s", err.Error())
		return
	}

	buf := make([]byte, 2)
	if n, err := oldScon.Read(buf); err != nil {
		fmt.Fprintf(os.Stderr, "Read failed: %s", err.Error())
		return
	} else {
		fmt.Fprintln(os.Stdout, string(buf[:n]))
	}

	fmt.Fprintln(os.Stdout, "close old connection")

	oldScon.Close()
	oldScon.Write([]byte("!!!! no dropped !!!!"))

	new, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connect failed: %s", err.Error())
		return
	}

	scon := scp.Client(new, oldScon)
	if _, err := scon.Write([]byte("world\n")); err != nil {
		fmt.Fprintf(os.Stderr, "Write failed: %s", err.Error())
		return
	}

	go io.Copy(os.Stdout, scon)
	if _, err := io.Copy(scon, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "Copy failed: %s", err.Error())
	}
}
