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

	Options struct {
		timeout   int
		fecData   int
		fecParity int
	}

	tcpListener struct {
		ln *net.TCPListener
	}

	kcpListener struct {
		ln *kcp.Listener
	}

	tcpConn struct {
		conn *net.TCPConn
	}

	kcpConn struct {
		conn *kcp.UDPSession
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
		return tcpListener{ln: ln}, nil
	}

	// kcp
	ln, err := kcp.ListenWithOptions(laddr, nil, options.fecData, options.fecParity)
	return kcpListener{ln: ln}, err
}

func (t tcpListener) Accept() (Conn, error) {
	conn, err := t.ln.AcceptTCP()
	return tcpConn{conn: conn}, err
}

func (k kcpListener) Accept() (Conn, error) {
	conn, err := k.ln.AcceptKCP()
	return kcpConn{conn: conn}, err
}

func (t tcpConn) SetOptions(options *Options) {
	t.conn.SetKeepAlive(true)
	t.conn.SetKeepAlivePeriod(time.Second * 60)
	t.conn.SetLinger(0)
}

func (t tcpConn) GetConn() net.Conn {
	return t.conn
}

func (k kcpConn) SetOptions(options *Options) {

}

func (k kcpConn) GetConn() net.Conn {
	return k.conn
}
