package main

import (
	"bytes"
	crand "crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
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
	originConn.Close()
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
		log.Printf("dail failed: connect=%s, err=%s", cc.connect, err.Error())
		return err
	}
	defer old.Close()

	n := optPackets / 2
	originConn := scp.Client(old, nil)
	if err = cc.testN(originConn, n); err != nil {
		log.Printf("testN failed: addr=%s, err=%s", old.LocalAddr(), err.Error())
		return err
	}

	new, err := Dial(network, cc.connect)
	if err != nil {
		log.Printf("dail failed: connect=%s, err=%s", cc.connect, err.Error())
		return err
	}
	defer new.Close()

	scon, err := cc.testSCP(originConn, new)
	if err != nil {
		log.Printf("testSCP failed: addr=%s, err=%s", new.LocalAddr(), err.Error())
		return err
	}
	defer scon.Close()

	if err = cc.testN(scon, optPackets-n); err != nil {
		log.Printf("testN failed: addr=%s, err=%s", new.LocalAddr(), err.Error())
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

func testN() {
	ch := make(chan error, optConcurrent)
	for i := 0; i < optConcurrent; i++ {
		go func() {
			cc := &ClientCase{
				connect: optConnect,
			}
			ch <- cc.Start()
		}()
	}

	for i := 0; i < optConcurrent; i++ {
		err := <-ch
		if err != nil {
			log.Printf("<%d>: %s\n", i, err.Error())
		}
	}
}

var optConnect string
var optConcurrent, optPackets, optPacketsPerSecond, optMinPacket, optMaxPacket int
var optRunRounds uint
var optVerbose bool
var network string
var fecData, fecParity int

func main() {
	var echoServer string

	flag.IntVar(&optConcurrent, "concurrent", 1, "concurrent connections")
	flag.IntVar(&optPackets, "packets", 100, "total packets each connection")
	flag.IntVar(&optPacketsPerSecond, "pps", 100, "packets per second each connection")
	flag.IntVar(&optMinPacket, "min", 50, "min packet size")
	flag.IntVar(&optMaxPacket, "max", 100, "max packet size")
	flag.UintVar(&optRunRounds, "rounds", 1, "run rounds")
	flag.StringVar(&echoServer, "startEchoServer", "", "start echo server")
	flag.StringVar(&optConnect, "connect", "127.0.0.1:1248", "connect to scon server")
	flag.BoolVar(&optVerbose, "verbose", false, "verbose")
	kcp := flag.NewFlagSet("kcp", flag.ExitOnError)
	kcp.IntVar(&fecData, "fec_data", 1, "FEC: number of shards to split the data into")
	kcp.IntVar(&fecParity, "fec_parity", 0, "FEC: number of parity shards")
	flag.Parse()

	log.SetFlags(log.Lshortfile | log.LstdFlags)

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
			log.Printf("start echo server: %s", err.Error())
			return
		}
		log.Print("run as echo server")
		log.Printf("listen %s", ln.Addr())
		ch := make(chan bool, 0)
		ch <- true
		return
	}

	if optConnect != "" {
		log.Print("run as echo client")
		log.Printf("config: server=%s, concurrent=%d, packets=%d, pps=%d, sz=[%d, %d]",
			optConnect, optConcurrent, optPackets, optPacketsPerSecond, optMinPacket, optMaxPacket)
		log.Printf("run test %d rounds", optRunRounds)
		var round uint
		for round = 1; round <= optRunRounds; round++ {
			log.Printf("round %d", round)
			testN()
		}
	}
}
