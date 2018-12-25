package main

import (
	"net"
	"time"

	kcp "github.com/xtaci/kcp-go"
)

type (
	// Listener 监听器
	Listener interface {
		Accept() (Conn, error)
	}

	// Conn 封装kcp和tcp的接口
	Conn interface {
		SetOptions(*Options)
		GetConn() net.Conn
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
	}

	Options struct {
		reuseTimeout int
		tcpOptions    *TcpOptions
		kcpOptions    *KcpOptions
	}

	tcpListener struct {
		ln *net.TCPListener

		options *TcpOptions
	}

	kcpListener struct {
		ln *kcp.Listener

		options *KcpOptions
	}

	NetTCPConn struct {
		*net.TCPConn
		readTimeout    int
	}

	tcpConn struct {
		conn *NetTCPConn
	}

	NetKCPConn struct {
		*kcp.UDPSession
		readTimeout    int
	}

	kcpConn struct {
		conn *NetKCPConn
	}
)

func ListenWithOptions(network, laddr string, options *Options) (Listener, error) {
	if network == "tcp" {
		tcpAddr, err := net.ResolveTCPAddr(network, laddr)
		if err != nil {
			return nil, err
		}

		ln, err := net.ListenTCP("tcp", tcpAddr)
		if err != nil {
			return nil, err
		}
		return tcpListener{ln: ln, options: options.tcpOptions}, nil
	}

	// kcp
	kcpOptions := options.kcpOptions
	ln, err := kcp.ListenWithOptions(laddr, nil, kcpOptions.fecData, kcpOptions.fecParity)
	return kcpListener{ln: ln, options: kcpOptions}, err
}

func (t tcpListener) Accept() (Conn, error) {
	conn, err := t.ln.AcceptTCP()
	return tcpConn{conn: &NetTCPConn{conn, t.options.readTimeout}}, err
}

func (k kcpListener) Accept() (Conn, error) {
	conn, err := k.ln.AcceptKCP()
	return kcpConn{conn: &NetKCPConn{conn, k.options.readTimeout}}, err
}

func (c *NetTCPConn) Read(b []byte) (n int, err error) {
	if c.readTimeout > 0 {
		timeout := time.Duration(c.readTimeout) * time.Second
		c.SetReadDeadline(time.Now().Add(timeout))
	}
	return c.TCPConn.Read(b)
}

func (t tcpConn) SetOptions(options *Options) {
	t.conn.SetKeepAlive(true)
	t.conn.SetKeepAlivePeriod(time.Second * 60)
	t.conn.SetLinger(0)
}

func (t tcpConn) GetConn() net.Conn {
	return t.conn
}

func (c *NetKCPConn) Read(b []byte) (n int, err error) {
	if c.readTimeout > 0 {
		timeout := time.Duration(c.readTimeout) * time.Second
		c.SetReadDeadline(time.Now().Add(timeout))
	}
	return c.UDPSession.Read(b)
}

func (k kcpConn) SetOptions(options *Options) {
	kcpOptions := options.kcpOptions
	k.conn.SetWindowSize(kcpOptions.sndWnd, kcpOptions.rcvWnd)
	k.conn.SetNoDelay(kcpOptions.nodelay, kcpOptions.interval, kcpOptions.resend, kcpOptions.nc)
}

func (k kcpConn) GetConn() net.Conn {
	return k.conn
}
