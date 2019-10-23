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
	"time"

	"github.com/ejoy/goscon/scp"
	"github.com/xtaci/kcp-go"
)

type ClientCase struct {
	connect string
}

func (cc *ClientCase) testEchoWrite(conn net.Conn, times int, ch chan<- []byte, done chan<- error) {
	interval := time.Second / time.Duration(optPacketsPerSecond)
	for i := 0; i < times; i++ {
		sz := mrand.Intn(optMaxPacket-optMinPacket) + optMinPacket
		buf := make([]byte, sz)
		crand.Read(buf[:sz])
		if _, err := conn.Write(buf[:sz]); err != nil {
			done <- err
		}
		ch <- buf[:sz]
		time.Sleep(interval)
	}
	close(ch)
	done <- nil
}

func (cc *ClientCase) testEchoRead(conn net.Conn, ch <-chan []byte, done chan<- error) {
	rbuf := make([]byte, optMaxPacket)
	for buf := range ch {
		sz := len(buf)
		if _, err := io.ReadFull(conn, rbuf[:sz]); err != nil {
			done <- err
		}
		if !bytes.Equal(buf[:sz], rbuf[:sz]) {
			done <- fmt.Errorf("echo unexpected<%d>:\nw:% x\nr:% x", sz, buf[:sz], rbuf[:sz])
		}
	}
	done <- nil
}

func (cc *ClientCase) testSCP(originConn *scp.Conn, conn net.Conn) (*scp.Conn, error) {
	sz := mrand.Intn(optMaxPacket-optMinPacket) + optMinPacket
	wbuf := make([]byte, sz)
	rbuf := make([]byte, sz)
	crand.Read(wbuf)

	originConn.Write(wbuf[:sz/2])
	//originConn.Close()
	originConn.Write(wbuf[sz/2:])

	scon := scp.Client(conn, &scp.Config{ConnForReused: originConn})
	if _, err := io.ReadFull(scon, rbuf); err != nil {
		return nil, err
	}

	if !bytes.Equal(wbuf, rbuf) {
		err := fmt.Errorf("testSCP<%s>:\nw:% x\nr:% x", scon.LocalAddr(), wbuf, rbuf)
		return nil, err
	}
	return scon, nil
}

func (cc *ClientCase) testN(conn *scp.Conn, packets int) error {
	ch := make(chan []byte, packets)
	done := make(chan error, 2)
	go cc.testEchoWrite(conn, packets, ch, done)
	go cc.testEchoRead(conn, ch, done)

	for i := 0; i < 2; i++ {
		err := <-done
		if err != nil {
			return err
		}
	}
	return nil
}

func Dial(network, connect string) (net.Conn, error) {
	if network == "tcp" {
		return net.Dial(network, connect)
	} else {
		return kcp.DialWithOptions(connect, nil, fecData, fecParity)
	}
}

func (cc *ClientCase) Start() error {
	old, err := Dial(network, cc.connect)
	if err != nil {
		return err
	}
	defer old.Close()

	n := optPackets / 2
	originConn := scp.Client(old, nil)
	if err = cc.testN(originConn, n); err != nil {
		return err
	}

	new, err := Dial(network, cc.connect)
	if err != nil {
		return err
	}
	defer new.Close()

	scon, err := cc.testSCP(originConn, new)
	if err != nil {
		return err
	}
	defer scon.Close()

	if err = cc.testN(scon, optPackets-n); err != nil {
		return err
	}
	return nil
}

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
				if optVerbose {
					wr := io.MultiWriter(c, os.Stdout)
					io.Copy(wr, c)
				} else {
					io.Copy(c, c)
				}
			}(conn)
		}
	}()
	return ln, nil
}

func testN(addr string) {
	ch := make(chan error, optConcurrent)
	for i := 0; i < optConcurrent; i++ {
		go func() {
			cc := &ClientCase{
				connect: addr,
			}
			ch <- cc.Start()
		}()
	}

	for i := 0; i < optConcurrent; i++ {
		err := <-ch
		if err != nil {
			fmt.Fprintf(os.Stderr, "<%d>: %s\n", i, err.Error())
		}
	}
}

var optConcurrent, optPackets, optPacketsPerSecond, optMinPacket, optMaxPacket int
var optVerbose bool
var network string
var fecData, fecParity int

func main() {
	var echoServer string
	var sconServer string

	flag.IntVar(&optConcurrent, "concurrent", 1, "concurrent connections")
	flag.IntVar(&optPackets, "packets", 100, "packets per connection")
	flag.IntVar(&optPacketsPerSecond, "pps", 100, "packets per second each connection")
	flag.IntVar(&optMinPacket, "min", 50, "min packet size")
	flag.IntVar(&optMaxPacket, "max", 100, "max packet size")
	flag.StringVar(&echoServer, "startEchoServer", "", "start echo server")
	flag.StringVar(&sconServer, "sconServer", "127.0.0.1:1248", "connect to scon server")
	flag.BoolVar(&optVerbose, "verbose", false, "verbose")
	kcp := flag.NewFlagSet("kcp", flag.ExitOnError)
	kcp.IntVar(&fecData, "fec_data", 1, "FEC: number of shards to split the data into")
	kcp.IntVar(&fecParity, "fec_parity", 0, "FEC: number of parity shards")
	flag.Parse()

	args := flag.Args()

	if len(args) > 0 && args[0] == "kcp" {
		kcp.Parse(args[1:])
		network = "kcp"
	} else {
		network = "tcp"
	}

	if echoServer != "" {
		ln, err := startEchoServer(echoServer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "start echo server: %s\n", err.Error())
			return
		}
		fmt.Fprintf(os.Stdout, "echo server: %s", ln.Addr())
		ch := make(chan bool, 0)
		ch <- true
		return
	}

	if sconServer != "" {
		testN(sconServer)
	}
}
