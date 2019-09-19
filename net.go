package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	reuse "github.com/libp2p/go-reuseport"
	"github.com/pkg/errors"
	kcp "github.com/ejoy/kcp-go"
)

type (
	Listener interface {
		Accept() (Conn, error)
	}

	Conn interface {
		net.Conn
		SetOptions(*Options)
	}

	TcpOptions struct {
		readTimeout int
	}

	KcpOptions struct {
		readTimeout int
		sndWnd      int
		rcvWnd      int
		nodelay     int
		interval    int
		resend      int
		nc          int // flow control
		fecData     int
		fecParity   int
		readBuffer  int
		writeBuffer int
		reuseport   int
	}

	Options struct {
		reuseTimeout     int
		handshakeTimeout int
		tcpOptions       *TcpOptions
		kcpOptions       *KcpOptions
	}

	tcpListener struct {
		ln *net.TCPListener

		options *TcpOptions
	}

	kcpListener struct {
		ln *kcp.Listener

		options *KcpOptions
	}

	tcpConn struct {
		*net.TCPConn
		readTimeout time.Duration
	}

	kcpConn struct {
		*kcp.UDPSession
		readTimeout time.Duration
	}

	kcpPacketConn struct {
		net.PacketConn
		fecHeaderSize int
	}
)

func (conn kcpPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, addr, err = conn.PacketConn.ReadFrom(p)
	if n < 4 {
		Error("receive invalid data from <%s> size %d", addr, n)
	} else {
		conv := binary.LittleEndian.Uint32(p[conn.fecHeaderSize:])
		Debug("<kcp:%s> conv %d recv %d", addr, conv, n)
	}
	return n, addr, err
}

func (conn kcpPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	n, err = conn.PacketConn.WriteTo(p, addr)
	conv := binary.LittleEndian.Uint32(p[conn.fecHeaderSize:])
	Debug("<kcp:%s> conv %d send %d", addr, conv, n)
	return n, err
}

func kcpListenWithOptions(laddr string, block kcp.BlockCrypt, dataShards, parityShards int) (*kcp.Listener, error) {
	conn, err := reuse.ListenPacket("udp", laddr)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	fecHeaderSize := 0
	if dataShards != 0 && parityShards != 0 {
		fecHeaderSize = 6
	}
	kcpconn := kcpPacketConn{conn, fecHeaderSize}

	return kcp.ServeConn(block, dataShards, parityShards, kcpconn)
}

func ListenWithOptions(network, laddr string, options *Options) (Listener, error) {
	if network == "tcp" {
		tcpAddr, err := net.ResolveTCPAddr(network, laddr)
		if err != nil {
			return nil, err
		}

		ln, err := net.ListenTCP("tcp", tcpAddr)
		if err != nil {
			fmt.Printf("%v", err)
			return nil, err
		}
		return tcpListener{ln: ln, options: options.tcpOptions}, nil
	}

	// kcp
	kcpOptions := options.kcpOptions

	ln, err := kcpListenWithOptions(laddr, nil, kcpOptions.fecData, kcpOptions.fecParity)
	if err != nil {
		return nil, err
	}
	ln.SetReadBuffer(kcpOptions.readBuffer)
	ln.SetWriteBuffer(kcpOptions.writeBuffer)
	ln.SetDSCP(46)
	return kcpListener{ln: ln, options: kcpOptions}, err
}

func (t tcpListener) Accept() (Conn, error) {
	conn, err := t.ln.AcceptTCP()
	timeout := time.Duration(t.options.readTimeout) * time.Second
	return &tcpConn{conn, timeout}, err
}

func (k kcpListener) Accept() (Conn, error) {
	conn, err := k.ln.AcceptKCP()
	timeout := time.Duration(k.options.readTimeout) * time.Second
	conn.SetStreamMode(true)
	Debug("accept new connection from: <kcp:%s> conv %d", conn.RemoteAddr(), conn.GetConv())
	return &kcpConn{conn, timeout}, err
}

func (t *tcpConn) Read(b []byte) (n int, err error) {
	if t.readTimeout > 0 {
		t.SetReadDeadline(time.Now().Add(t.readTimeout))
	}
	return t.TCPConn.Read(b)
}

func (t *tcpConn) SetOptions(options *Options) {
	t.SetKeepAlive(true)
	t.SetKeepAlivePeriod(time.Second * 60)
	t.SetLinger(0)
}

func (k *kcpConn) Read(b []byte) (n int, err error) {
	if k.readTimeout > 0 {
		k.SetReadDeadline(time.Now().Add(k.readTimeout))
	}
	return k.UDPSession.Read(b)
}

func (k *kcpConn) SetOptions(options *Options) {
	kcpOptions := options.kcpOptions
	k.SetWindowSize(kcpOptions.sndWnd, kcpOptions.rcvWnd)
	k.SetNoDelay(kcpOptions.nodelay, kcpOptions.interval, kcpOptions.resend, kcpOptions.nc)
}
