package main

import (
	"encoding/binary"
	"net"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-reuseport"
	"github.com/xjdrew/glog"
	"github.com/xtaci/kcp-go"
)

type kcpConn struct {
	*kcp.UDPSession
	readTimeout time.Duration
}

// Read .
func (conn *kcpConn) Read(b []byte) (int, error) {
	if conn.readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(conn.readTimeout))
	}
	return conn.UDPSession.Read(b)
}

// kcpPacketConn 代表一个 udp fd
type kcpPacketConn struct {
	net.PacketConn
	fecHeaderSize int
}

// ReadFrom .
func (pconn kcpPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, addr, err = pconn.PacketConn.ReadFrom(p)
	if glog.V(2) {
		if n >= pconn.fecHeaderSize+4 {
			conv := binary.LittleEndian.Uint32(p[pconn.fecHeaderSize:])
			glog.Infof("receive packet: addr=%s, conv=%d, len=%d", addr, conv, n)
		} else {
			glog.Errorf("receive invalid packet: addr=%s, len=%d", addr, n)
		}
	}
	return n, addr, err
}

func (pconn kcpPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	n, err = pconn.PacketConn.WriteTo(p, addr)
	if glog.V(2) {
		if n >= pconn.fecHeaderSize+4 {
			conv := binary.LittleEndian.Uint32(p[pconn.fecHeaderSize:])
			glog.Infof("send packet: addr=%s, conv=%d, len=%d", addr.String(), conv, n)
		}
	}
	return n, err
}

// KCPListener .
type KCPListener struct {
	*kcp.Listener
	option atomic.Value
}

// SetOption .
func (l *KCPListener) SetOption(option *KCPOption) {
	l.option.Store(option)
}

// Accept .
func (l *KCPListener) Accept() (net.Conn, error) {
	conn, err := l.AcceptKCP()
	if err != nil {
		return nil, err
	}
	if glog.V(1) {
		glog.Infof("accept new kcp connection: addr=%s, conv=%d", conn.RemoteAddr(), conn.GetConv())
	}

	// set kcp option
	option := l.option.Load().(*KCPOption)

	mtu := option.OptMTU
	nodelay := option.OptNodelay
	interval := option.OptInterval
	resend := option.OptResend
	nc := option.OptNC
	sndwnd := option.OptSndwnd
	rcvwnd := option.OptRcvwnd
	stream := option.OptStream
	writedelay := option.OptWriteDelay

	conn.SetMtu(mtu)
	conn.SetWindowSize(sndwnd, rcvwnd)
	conn.SetNoDelay(nodelay, interval, resend, nc)
	conn.SetStreamMode(stream)
	conn.SetWriteDelay(writedelay)

	readTimeout := option.ReadTimeout * time.Second
	return &kcpConn{conn, readTimeout}, err
}

// NewKCPListener creates a new KCPListener
func NewKCPListener(laddr string, option *KCPOption) (*KCPListener, error) {
	conn, err := reuseport.ListenPacket("udp", laddr)
	if err != nil {
		glog.Errorf("new kcp listener failed: %s", err.Error())
		return nil, err
	}

	fecDataShards := option.FecDataShards
	fecParityShards := option.FecParityShards

	fecHeaderSize := 0
	if fecDataShards != 0 && fecParityShards != 0 {
		// magic number: fec header size
		fecHeaderSize = 6
	}
	packetConn := kcpPacketConn{conn, fecHeaderSize}

	ln, err := kcp.ServeConn(nil, fecDataShards, fecParityShards, packetConn)
	if err != nil {
		return nil, err
	}

	readBuffer := option.ReadBuffer
	writeBuffer := option.WriteBuffer
	ln.SetReadBuffer(readBuffer)
	ln.SetWriteBuffer(writeBuffer)

	// ?
	// ln.SetDSCP(46)
	l := &KCPListener{Listener: ln}
	l.option.Store(option)
	return l, nil
}
