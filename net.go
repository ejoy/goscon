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
		readTimeout    time.Duration
	}

	kcpConn struct {
		*kcp.UDPSession
		readTimeout    time.Duration
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
	timeout := time.Duration(t.options.readTimeout) * time.Second
	return &tcpConn{conn, timeout}, err
}

func (k kcpListener) Accept() (Conn, error) {
	conn, err := k.ln.AcceptKCP()
	timeout := time.Duration(k.options.readTimeout) * time.Second
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
