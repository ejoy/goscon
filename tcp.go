package main

import (
	"net"
	"sync/atomic"
	"time"

	"github.com/xjdrew/glog"
)

type tcpConn struct {
	*net.TCPConn
	readTimeout time.Duration
}

func (conn tcpConn) Read(b []byte) (int, error) {
	if conn.readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(conn.readTimeout))
	}
	return conn.TCPConn.Read(b)
}

// TCPListener .
type TCPListener struct {
	net.Listener
	option atomic.Value
}

// SetOption .
func (l *TCPListener) SetOption(option *TCPOption) {
	l.option.Store(option)
}

// Accept .
func (l *TCPListener) Accept() (conn net.Conn, err error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return
	}

	if glog.V(1) {
		glog.Infof("accept new tcp connection: addr=%s", c.RemoteAddr())
	}

	option := l.option.Load().(*TCPOption)
	keepalive := option.Keepalive
	keepaliveInterval := option.KeepaliveInterval * time.Second
	readTimeout := option.ReadTimeout * time.Second

	t := c.(*net.TCPConn)
	t.SetKeepAlive(keepalive)
	t.SetKeepAlivePeriod(keepaliveInterval)
	// t.SetLinger(0)

	conn = tcpConn{t, readTimeout}
	return
}

// NewTCPListener creates a new TCPListener
func NewTCPListener(laddr string, option *TCPOption) (*TCPListener, error) {
	ln, err := net.Listen("tcp", laddr)
	if err != nil {
		return nil, err
	}
	l := &TCPListener{Listener: ln}
	l.option.Store(option)
	return l, nil
}
