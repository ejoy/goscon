package main

import (
	"bytes"
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/ejoy/goscon/scp"
	"github.com/ejoy/goscon/ws"
	"github.com/xjdrew/glog"
	sproto "github.com/xjdrew/gosproto"
	"github.com/xtaci/kcp-go"
)

type ClientCase struct {
	connect string
}

type sprotoPackage struct {
	Type    int32  `sproto:"integer,0,name=type"`
	Session *int32 `sproto:"integer,1,name=session"`
	Ud      int32  `sproto:"integer,2,name=ud"`
}

type sprotoAnnounceAddr struct {
	RemoteAddr string `sproto:"string,0,name=remote_addr"`
	LocalAddr  string `sproto:"string,1,name=local_addr"`
}

func packetSize() int {
	sz := optMinPacket
	if optMaxPacket > optMinPacket {
		sz = sz + mrand.Intn(optMaxPacket-optMinPacket)
	}
	return sz
}

func (cc *ClientCase) testEchoWrite(conn net.Conn, times int, ch chan<- []byte, done chan<- error) {
	interval := time.Second / time.Duration(optPacketsPerSecond)
	for i := 0; i < times; i++ {
		sz := packetSize()
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
	sz := packetSize()
	wbuf := make([]byte, sz)
	rbuf := make([]byte, sz)
	crand.Read(wbuf)

	originConn.Write(wbuf[:sz/2])
	originConn.Write(wbuf[sz/2:])

	scon, err := scp.Client(conn, &scp.Config{ConnForReused: originConn})
	if err != nil {
		glog.Errorf("create reuse client failed: addr=%s, err=%s", conn.LocalAddr(), err.Error())
		return nil, err
	}
	originConn.Close()

	if _, err := io.ReadFull(scon, rbuf); err != nil {
		glog.Errorf("testSCP read scon failed: addr=%s, err=%s", conn.LocalAddr(), err.Error())
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
	} else if network == "kcp" {
		return kcp.DialWithOptions(connect, nil, fecData, fecParity)
	} else if network == "ws" {
		return ws.Dial(connect)
	} else {
		return nil, errors.New("Invalid network")
	}
}

func (cc *ClientCase) Start() error {
	n := optPackets / optReuses
	if n <= 0 {
		n = 1
	}

	raw, err := Dial(network, cc.connect)
	if err != nil {
		glog.Errorf("dail failed: connect=%s, err=%s", cc.connect, err.Error())
		return err
	}
	preConn, _ := scp.Client(raw, nil)

	for i := 0; i < optReuses; i++ {
		if err = cc.testN(preConn, n); err != nil {
			glog.Errorf("testN failed: addr=%s, err=%s", preConn.LocalAddr(), err.Error())
			return err
		}

		new, err := Dial(network, cc.connect)
		if err != nil {
			glog.Errorf("dail failed: connect=%s, err=%s", cc.connect, err.Error())
			return err
		}

		nextConn, err := cc.testSCP(preConn, new)
		if err != nil {
			return err
		}
		preConn = nextConn
	}
	preConn.Close()
	return nil
}

func readRemoteAddressPacket(c net.Conn) error {
	var sz uint16
	if err := binary.Read(c, binary.BigEndian, &sz); err != nil {
		return err
	}
	addressBuf := make([]byte, sz)
	if _, err := io.ReadFull(c, addressBuf); err != nil {
		return err
	}
	unpacked, err := sproto.Unpack(addressBuf)
	if err != nil {
		return err
	}
	spHeader := sprotoPackage{}
	spBody := sprotoAnnounceAddr{}
	used := sproto.MustDecode(unpacked, &spHeader)
	sproto.MustDecode(unpacked[used:], &spBody)
	glog.Infof("new connection, remote address: %s, local address: %s", spBody.RemoteAddr, spBody.LocalAddr)
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
				if optSproto {
					if err := readRemoteAddressPacket(c); err != nil {
						glog.Errorf("read remote address failed, err=%s", err.Error())
						return
					}
				}

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
			glog.Errorf("<%d>: %s\n", i, err.Error())
		}
	}
}

var optConnect string
var optConcurrent, optPackets, optPacketsPerSecond, optMinPacket, optMaxPacket int
var optReuses int
var optRunRounds uint
var optVerbose bool
var network string
var optTargetServer string
var optSproto bool
var fecData, fecParity int

func main() {
	// set default log directory
	flag.Set("log_dir", "./")
	flag.Set("logtostderr", "true")

	var echoServer string
	var optEchoClient bool

	flag.IntVar(&optConcurrent, "concurrent", 1, "concurrent connections")
	flag.IntVar(&optPackets, "packets", 100, "total packets each connection")
	flag.IntVar(&optPacketsPerSecond, "pps", 100, "packets per second each connection")
	flag.IntVar(&optMinPacket, "min", 50, "min packet size")
	flag.IntVar(&optMaxPacket, "max", 100, "max packet size")
	flag.IntVar(&optReuses, "reuse", 1, "reuse times each connection")
	flag.UintVar(&optRunRounds, "rounds", 1, "run rounds")
	flag.StringVar(&echoServer, "startEchoServer", "", "start echo server")
	flag.BoolVar(&optSproto, "sproto", false, "using sproto")
	flag.StringVar(&optConnect, "connect", "127.0.0.1:1248", "connect to scon server")
	flag.BoolVar(&optEchoClient, "startEchoClient", false, "start echo client")
	flag.BoolVar(&optVerbose, "verbose", false, "verbose")
	flag.StringVar(&optTargetServer, "targetServer", "", "prefered targetserver")
	kcp := flag.NewFlagSet("kcp", flag.ExitOnError)
	kcp.IntVar(&fecData, "fec_data", 1, "FEC: number of shards to split the data into")
	kcp.IntVar(&fecParity, "fec_parity", 0, "FEC: number of parity shards")
	flag.Parse()

	if optMinPacket > optMaxPacket {
		optMinPacket, optMaxPacket = optMaxPacket, optMinPacket
	}

	args := flag.Args()

	if len(args) == 0 {
		network = "tcp"
	} else if args[0] == "kcp" {
		kcp.Parse(args[1:])
		network = "kcp"
	} else if args[0] == "ws" {
		flag.CommandLine.Parse(args[1:])
		network = "ws"
	}

	if echoServer != "" {
		ln, err := startEchoServer(echoServer)
		if err != nil {
			glog.Errorf("start echo server: %s", err.Error())
			return
		}
		glog.Info("run as echo server")
		glog.Infof("listen %s", ln.Addr())
		ch := make(chan bool, 0)
		ch <- true
		return
	}

	if optEchoClient {
		conn, err := Dial(network, optConnect)
		if err != nil {
			glog.Errorf("start echo client: %s", err.Error())
			return
		}
		scon, _ := scp.Client(conn, &scp.Config{TargetServer: optTargetServer})
		go io.Copy(os.Stdout, scon)
		io.Copy(scon, os.Stdin)
		return
	}

	go func() {
		ln, err := net.Listen("tcp", ":0")
		if err != nil {
			glog.Errorf("listen failed: err=%v", err)
			return
		}
		glog.Infof("http listen: %s", ln.Addr())
		http.Serve(ln, nil)
	}()

	if optConnect != "" {
		glog.Info("run as echo client")
		glog.Infof("config: server=%s, concurrent=%d, packets=%d, pps=%d, sz=[%d, %d], reuses=%d",
			optConnect, optConcurrent, optPackets, optPacketsPerSecond, optMinPacket, optMaxPacket, optReuses)
		glog.Infof("run test %d rounds", optRunRounds)
		var round uint
		for round = 1; round <= optRunRounds; round++ {
			glog.Infof("round %d", round)
			testN()
		}
	}
}
