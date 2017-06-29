package main

import (
	"bytes"
	crand "crypto/rand"
	"flag"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"os"

	"github.com/ejoy/goscon/scp"
)

const (
	PACKSIZE_MIN = 50
	PACKSIZE_MAX = 100
)

func startEchoServer(laddr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", laddr)
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				break
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()
	return ln, nil
}

func testEchoWrite(conn net.Conn, times int, ch chan<- []byte, done chan<- error) {
	for i := 0; i < times; i++ {
		sz := mrand.Intn(PACKSIZE_MAX-PACKSIZE_MIN) + PACKSIZE_MIN
		buf := make([]byte, sz)
		crand.Read(buf[:sz])
		if _, err := conn.Write(buf[:sz]); err != nil {
			done <- err
		}
		ch <- buf[:sz]
	}
	close(ch)
	done <- nil
}

func testEchoRead(conn net.Conn, ch <-chan []byte, done chan<- error) {
	rbuf := make([]byte, PACKSIZE_MAX)
	for buf := range ch {
		sz := len(buf)
		crand.Read(rbuf[:sz])
		if _, err := io.ReadFull(conn, rbuf[:sz]); err != nil {
			done <- err
		}
		if !bytes.Equal(buf[:sz], rbuf[:sz]) {
			done <- fmt.Errorf("echo unexpected<%d>:\nw:% x\nr:% x", sz, buf[:sz], rbuf[:sz])
		}
	}
	done <- nil
}

func testEcho(conn net.Conn) error {
	times := 1000
	ch := make(chan []byte, times)
	done := make(chan error, 2)
	go testEchoWrite(conn, times, ch, done)
	go testEchoRead(conn, ch, done)

	for i := 0; i < 2; i++ {
		err := <-done
		if err != nil {
			return err
		}
	}
	return nil
}

func testScon(originConn *scp.Conn, conn net.Conn) (*scp.Conn, error) {
	buflen := 100
	wbuf := make([]byte, buflen)
	rbuf := make([]byte, buflen)
	crand.Read(wbuf)

	originConn.Write(wbuf[:buflen/2])
	originConn.Close()
	originConn.Write(wbuf[buflen/2:])

	scon := scp.Client(conn, originConn)
	if _, err := io.ReadFull(scon, rbuf); err != nil {
		return nil, err
	}

	if !bytes.Equal(wbuf, rbuf) {
		return nil, fmt.Errorf("scon unexpected:\nw:% x\nr:% x", wbuf, rbuf)
	}
	return scon, nil
}

func testSconServer(addr string) error {
	old, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer old.Close()

	originConn := scp.Client(old, nil)
	if err = testEcho(originConn); err != nil {
		return err
	}

	new, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer new.Close()

	scon, err := testScon(originConn, new)
	if err != nil {
		return err
	}

	if err = testEcho(scon); err != nil {
		return err
	}
	return nil
}

func testN(addr string, times int) {
	ch := make(chan error, times)
	for i := 0; i < times; i++ {
		go func() {
			ch <- testSconServer(addr)
		}()
	}

	for i := 0; i < times; i++ {
		err := <-ch
		if err != nil {
			fmt.Fprintf(os.Stderr, "testSconServer: %s\n", err.Error())
		}
	}
}

func main() {
	var echoServer string
	var sconServer string
	var concurrent int

	flag.IntVar(&concurrent, "concurrent", 1, "concurrent connections")
	flag.StringVar(&echoServer, "startEchoServer", "", "start echo server")
	flag.StringVar(&sconServer, "sconServer", "127.0.0.1:1248", "connect to scon server")
	flag.Parse()

	if echoServer != "" {
		ln, err := startEchoServer(echoServer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "start echo server: %s", err.Error())
			return
		}
		fmt.Fprintf(os.Stdout, "echo server: %s", ln.Addr())
		ch := make(chan bool, 0)
		ch <- true
		return
	}

	if sconServer != "" {
		testN(sconServer, concurrent)
	}
}
