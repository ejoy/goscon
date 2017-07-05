package main

import (
	"net"
	"sync"
	"time"

	"github.com/ejoy/goscon/scp"
	"io"
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

func (ss *SCPServer) handleClient(conn *net.TCPConn) {
	defer Recover()

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
