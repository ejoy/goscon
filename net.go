package main

import (
	"net"
	"time"

	kcp "github.com/xtaci/kcp-go"
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

	tcpConn struct {
		*net.TCPConn
		readTimeout    int
	}

	kcpConn struct {
		*kcp.UDPSession
		readTimeout    int
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
	return &tcpConn{conn, t.options.readTimeout}, err
}

func (k kcpListener) Accept() (Conn, error) {
	conn, err := k.ln.AcceptKCP()
	return &kcpConn{conn, k.options.readTimeout}, err
}

func (t *tcpConn) Read(b []byte) (n int, err error) {
	if t.readTimeout > 0 {
		timeout := time.Duration(t.readTimeout) * time.Second
		t.SetReadDeadline(time.Now().Add(timeout))
	}
	return t.TCPConn.Read(b)
}

func (t *tcpConn) SetOptions(options *Options) {
	t.SetKeepAlive(true)
	t.SetKeepAlivePeriod(time.Second * 60)
	t.SetLinger(0)
}

func (c *kcpConn) Read(b []byte) (n int, err error) {
	if c.readTimeout > 0 {
		timeout := time.Duration(c.readTimeout) * time.Second
		c.SetReadDeadline(time.Now().Add(timeout))
	}
	return c.UDPSession.Read(b)
}

func (k *kcpConn) SetOptions(options *Options) {
	kcpOptions := options.kcpOptions
	k.SetWindowSize(kcpOptions.sndWnd, kcpOptions.rcvWnd)
	k.SetNoDelay(kcpOptions.nodelay, kcpOptions.interval, kcpOptions.resend, kcpOptions.nc)
}
