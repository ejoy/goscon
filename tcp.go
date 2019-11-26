package main

import (
	"net"
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

	keepalive := configItemBool("tcp_option.keepalive")
	keepaliveInterval := configItemTime("tcp_option.keepalive_interval")
	readTimeout := configItemTime("tcp_option.read_timeout")

	t := c.(*net.TCPConn)
	t.SetKeepAlive(keepalive)
	t.SetKeepAlivePeriod(keepaliveInterval)
	// t.SetLinger(0)

	conn = tcpConn{t, readTimeout}
	return
}

// NewTCPListener creates a new TCPListener
func NewTCPListener(laddr string) (*TCPListener, error) {
	ln, err := net.Listen("tcp", laddr)
	if err != nil {
		return nil, err
	}
	return &TCPListener{ln}, nil
}
