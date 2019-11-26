package main

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/golang/glog"
	"github.com/libp2p/go-reuseport"
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
	mtu := configItemInt("kcp_option.opt_mtu")
	nodelay := configItemInt("kcp_option.opt_nodelay")
	interval := configItemInt("kcp_option.opt_interval")
	resend := configItemInt("kcp_option.opt_resend")
	nc := configItemInt("kcp_option.opt_nc")
	sndwnd := configItemInt("kcp_option.opt_sndwnd")
	rcvwnd := configItemInt("kcp_option.opt_rcvwnd")
	stream := configItemBool("kcp_option.opt_stream")

	conn.SetMtu(mtu)
	conn.SetWindowSize(sndwnd, rcvwnd)
	conn.SetNoDelay(nodelay, interval, resend, nc)
	conn.SetStreamMode(stream)

	readTimeout := configItemTime("kcp_option.read_timeout")
	return &kcpConn{conn, readTimeout}, err
}

// NewKCPListener creates a new KCPListener
func NewKCPListener(laddr string) (*KCPListener, error) {
	conn, err := reuseport.ListenPacket("udp", laddr)
	if err != nil {
		glog.Errorf("new kcp listener failed: %s", err.Error())
		return nil, err
	}

	fecDataShards := configItemInt("kcp_option.fec_data_shards")
	fecParityShards := configItemInt("kcp_option.fec_parity_shards")

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

	readBuffer := configItemInt("kcp_option.read_buffer")
	writeBuffer := configItemInt("kcp_option.write_buffer")
	ln.SetReadBuffer(readBuffer)
	ln.SetWriteBuffer(writeBuffer)

	// ?
	// ln.SetDSCP(46)
	return &KCPListener{ln}, nil
}
