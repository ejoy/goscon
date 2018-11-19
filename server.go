package main

import (
	"net"
	"sync"
	"time"

	"io"

	"github.com/ejoy/goscon/scp"
	"github.com/xtaci/kcp-go"
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

func downloadUntilClose(dst HalfCloseConn, src HalfCloseConn, ch chan<- int) error {
	var err error
	var written, packets int
	buf := make([]byte, scp.NetBufferSize)
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				packets++
				written += nw
			}
			if ew != nil {
				err = ew
				break
			}
		}
		if er != nil {
			err = er
			break
		}
	}
	src.CloseRead()
	dst.CloseWrite()
	ch <- written
	ch <- packets
	return err
}

func uploadUntilClose(dst HalfCloseConn, src HalfCloseConn, ch chan<- int) error {
	var err error
	var written, packets int
	buf := make([]byte, scp.NetBufferSize)

	delay := time.Duration(optUploadMaxDelay) * time.Millisecond

	for {
		var nr int
		var er error
		if optUploadMinPacket > 0 && delay > 0 {
			src.SetReadDeadline(time.Now().Add(delay))
			nr, er = io.ReadAtLeast(src, buf, optUploadMinPacket)
		} else {
			nr, er = src.Read(buf)
		}

		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				packets++
				written += nw
			}
			if ew != nil {
				err = ew
				break
			}
		}
		if er != nil {
			if netError, ok := er.(net.Error); ok && netError.Timeout() {
				continue
			}
			err = er
			break
		}
	}
	src.CloseRead()
	dst.CloseWrite()
	ch <- written
	ch <- packets
	return err
}

func (p *ConnPair) Reuse(scon *scp.Conn) {
	Info("<%d> reuse, change remote from [%s><%s] to [%s><%s]", p.RemoteConn.ID(), p.RemoteConn.RemoteAddr(), p.RemoteConn.LocalAddr(), scon.LocalAddr(), scon.RemoteAddr())
	p.RemoteConn.SetConn(scon)
}

func (p *ConnPair) Pump() {
	Info("<%d> new pair [%s><%s] [%s><%s]", p.RemoteConn.ID(), p.RemoteConn.RemoteAddr(), p.RemoteConn.LocalAddr(), p.LocalConn.LocalAddr(), p.LocalConn.RemoteAddr())
	downloadCh := make(chan int)
	uploadCh := make(chan int)

	go downloadUntilClose(p.LocalConn, p.RemoteConn, downloadCh)
	go uploadUntilClose(p.RemoteConn, p.LocalConn, uploadCh)

	dlData := <-downloadCh
	dlPackets := <-downloadCh
	dlSize := 0
	if dlData > 0 {
		dlSize = dlData / dlPackets
	}
	ulData := <-uploadCh
	ulPackets := <-uploadCh
	ulSize := 0
	if ulData > 0 {
		ulSize = ulData / ulPackets
	}
	Info("<%d> remove pair [%s><%s] [%s><%s], download:(%d:%d:%d), upload:(%d:%d:%d)", p.RemoteConn.ID(),
		p.RemoteConn.RemoteAddr(), p.RemoteConn.LocalAddr(), p.LocalConn.LocalAddr(), p.LocalConn.RemoteAddr(),
		dlData, dlPackets, dlSize, ulData, ulPackets, ulSize)
}

type SCPServer struct {
	network      string
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
	pair := ss.GetConnPair(id)

	if pair != nil {
		pair.RemoteConn.CloseForReuse()
		return pair.RemoteConn.RawConn()
	}
	return nil
}

func (ss *SCPServer) AddConnPair(id int, pair *ConnPair) {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	if _, ok := ss.connPairs[id]; ok {
		Panic("ConnPair conflict: id<%d>", id)
	}
	ss.connPairs[id] = pair
}

func (ss *SCPServer) RemoveConnPair(id int) {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	delete(ss.connPairs, id)
}

func (ss *SCPServer) GetConnPair(id int) *ConnPair {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	return ss.connPairs[id]
}

func (ss *SCPServer) onReusedConn(scon *scp.Conn) {
	id := scon.ID()
	pair := ss.GetConnPair(id)

	if pair != nil {
		pair.Reuse(scon)
	}
}

func (ss *SCPServer) onNewConn(scon *scp.Conn) {
	id := scon.ID()
	defer ss.ReleaseID(id)

	connPair := &ConnPair{}
	connPair.RemoteConn = NewSCPConn(scon, ss.reuseTimeout)
	// hold conn pair for reuse
	ss.AddConnPair(id, connPair)
	defer ss.RemoveConnPair(id)

	localConn, err := glbLocalConnProvider.CreateLocalConn(scon)
	if err != nil {
		scon.Close()
		Error("create local connnection failed: %s", err.Error())
		return
	}

	connPair.LocalConn = localConn
	connPair.Pump()
}

type (
	// Lisner 监听器
	Lisner interface {
		Accept() (IConn, error)
	}

	// IConn 封装kcp和tcp的接口
	IConn interface {
		SetOptions()
		GetConn() net.Conn
	}

	tcpListen struct {
		ln *net.TCPListener
	}

	kcpListen struct {
		ln *kcp.Listener
	}

	tcpConn struct {
		conn *net.TCPConn
	}
	kcpConn struct {
		conn *kcp.UDPSession
	}
)

func newLisner(network, laddr string) (Lisner, error) {
	if network == "tcp" {
		tcpAddr, err := net.ResolveTCPAddr("tcp", laddr)
		if err != nil {
			return nil, err
		}

		ln, err := net.ListenTCP("tcp", tcpAddr)
		if err != nil {
			return nil, err
		}
		return tcpListen{ln: ln}, nil
	}

	// kcp
	ln, err := kcp.ListenWithOptions(laddr, nil, 1, 0)
	return kcpListen{ln: ln}, err
}

func (t tcpListen) Accept() (IConn, error) {
	conn, err := t.ln.AcceptTCP()
	return tcpConn{conn: conn}, err
}

func (k kcpListen) Accept() (IConn, error) {
	conn, err := k.ln.AcceptKCP()
	return kcpConn{conn: conn}, err
}

func (t tcpConn) SetOptions() {
	t.conn.SetKeepAlive(true)
	t.conn.SetKeepAlivePeriod(time.Second * 60)
	t.conn.SetLinger(0)
}

func (t tcpConn) GetConn() net.Conn {
	return t.conn
}

func (k kcpConn) SetOptions() {

}

func (k kcpConn) GetConn() net.Conn {
	return k.conn
}

func (ss *SCPServer) handleClient(iconn IConn) {
	defer Recover()
	conn := iconn.GetConn()
	scon := scp.Server(conn, &scp.Config{ScpServer: ss})
	if err := scon.Handshake(); err != nil {
		Error("handshake error [%s]: %s", conn.RemoteAddr().String(), err.Error())
		conn.Close()
		return
	}

	iconn.SetOptions()

	if scon.IsReused() {
		ss.onReusedConn(scon)
	} else {
		ss.onNewConn(scon)
	}
}

// Start process connections
func (ss *SCPServer) Start() error {
	ln, err := newLisner(ss.network, ss.laddr)
	if err != nil {
		return err
	}

	Info("scpServer listen: %s", ss.laddr)

	var tempDelay time.Duration // how long to sleep on accept failure

	for {
		conn, err := ln.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				Error("accept error: %v; retrying in %v", err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			Error("accept failed:%s", err.Error())
			return err
		}
		tempDelay = 0
		go ss.handleClient(conn)
	}
}

func NewSCPServer(network, laddr string, reuseTimeout int) *SCPServer {
	return &SCPServer{
		network:      network,
		laddr:        laddr,
		reuseTimeout: time.Duration(reuseTimeout) * time.Second,
		idAllocator:  scp.NewIDAllocator(1),
		connPairs:    make(map[int]*ConnPair),
	}
}
