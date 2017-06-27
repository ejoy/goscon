package main

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/ejoy/goscon/scp"
)

var ReuseTimeout = 300 * time.Second

type HalfCloseConn interface {
	net.Conn
	CloseRead() error
	CloseWrite() error
}

type ConnPair struct {
	LocalConn  *net.TCPConn // scp server <-> local server
	RemoteConn *SCPConn     // client <-> scp server
}

func copyUntilClose(dst HalfCloseConn, src HalfCloseConn, ch chan<- int64) error {
	n, err := io.Copy(dst, src)
	src.CloseRead()
	dst.CloseWrite()
	ch <- n
	return err
}

func (p *ConnPair) Pump() {
	Info("<%d> new pair [%s:%s] [%s:%s]", p.RemoteConn.ID(), p.RemoteConn.RemoteAddr(), p.RemoteConn.LocalAddr(), p.LocalConn.LocalAddr(), p.LocalConn.RemoteAddr())
	downloadCh := make(chan int64)
	uploadCh := make(chan int64)
	go copyUntilClose(p.LocalConn, p.RemoteConn, downloadCh)
	go copyUntilClose(p.RemoteConn, p.LocalConn, uploadCh)
	download := <-downloadCh
	upload := <-uploadCh
	Info("<%d> end, download:%d, upload:%d", p.RemoteConn.ID(), download, upload)
}

type SCPServer struct {
	laddr        string
	reuseTimeout time.Duration
	idAllocator  *scp.IDAllocator

	connPairMutex sync.Mutex
	connPairs     map[int]*ConnPair
}

func (ss *SCPServer) AcquireID() int {
	return ss.idAllocator.AcquireID()
}

func (ss *SCPServer) ReleaseID(id int) {
	ss.idAllocator.ReleaseID(id)
}

func (ss *SCPServer) QueryByID(id int) *scp.Conn {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	pair := ss.connPairs[id]
	if pair != nil {
		return pair.RemoteConn.RawConn()
	}
	return nil
}

func (ss *SCPServer) NumOfConnPairs() int {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	return len(ss.connPairs)
}

func (ss *SCPServer) CloseByID(id int) *scp.Conn {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	pair := ss.connPairs[id]
	if pair != nil {
		pair.RemoteConn.CloseForReuse()
		return pair.RemoteConn.RawConn()
	}
	return nil
}

func (ss *SCPServer) onReusedConn(conn *scp.Conn) {
}

func (ss *SCPServer) onNewConn(conn *scp.Conn) {
	defer ss.ReleaseID(conn.ID())
}

func (ss *SCPServer) handleClient(conn *net.TCPConn) {
	scon := scp.Server(conn, ss)
	if err := scon.Handshake(); err != nil {
		Error("handshake error [%s]: %s", conn.RemoteAddr().String(), err.Error())
		conn.Close()
		return
	}

	conn.SetKeepAlive(true)
	conn.SetKeepAlivePeriod(time.Second * 60)
	conn.SetLinger(0)

	if scon.IsReused() {
		ss.onReusedConn(scon)
	} else {
		ss.onNewConn(scon)
	}
}

func (ss *SCPServer) Start() error {
	tcpAddr, err := net.ResolveTCPAddr("tcp", ss.laddr)
	if err != nil {
		return err
	}

	ln, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		return err
	}

	Info("scpServer listen: %s", tcpAddr.String())

	for {
		conn, err := ln.AcceptTCP()
		if err != nil {
			Error("accept failed:%s", err.Error())
			if opErr, ok := err.(*net.OpError); ok {
				if !opErr.Temporary() {
					break
				}
			}
			continue
		}
		go ss.handleClient(conn)
	}
	return nil
}

func NewSCPServer(laddr string, reuseTimeout int) *SCPServer {
	return &SCPServer{
		laddr:        laddr,
		reuseTimeout: time.Duration(reuseTimeout) * time.Second,
		idAllocator:  scp.NewIDAllocator(1),
		connPairs:    make(map[int]*ConnPair),
	}
}
