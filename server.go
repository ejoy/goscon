package main

import (
	"net"
	"sync"
	"time"

	"github.com/ejoy/goscon/scp"
	"github.com/ejoy/goscon/upstream"
	"github.com/golang/glog"
)

type ConnPair struct {
	LocalConn  net.Conn // scp server <-> local server
	RemoteConn *SCPConn // client <-> scp server
}

func copyUntilError(tag string, dst net.Conn, src net.Conn, ch chan<- int) error {
	var err error
	var written, packets int
	buf := make([]byte, scp.NetBufferSize)
	addr := src.RemoteAddr()
	for {
		nr, er := src.Read(buf)
		if glog.V(2) {
			glog.Infof("recv packet: tag=%s, addr=%s, sz=%d", tag, addr, nr)
		}
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if glog.V(2) {
				glog.Infof("send packet: tag=%s, addr=%s, sz=%d", tag, addr, nw)
			}
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

	if glog.V(1) {
		glog.Infof("copyUntilError return: tag=%s err=%v", tag, err)
	}

	if halfConn, ok := src.(closeReader); ok {
		halfConn.CloseRead()
	} else {
		src.Close()
	}

	if halfConn, ok := dst.(closeWriter); ok {
		halfConn.CloseWrite()
	} else {
		dst.Close()
	}

	ch <- written
	ch <- packets
	return err
}

func (p *ConnPair) Reuse(scon *scp.Conn) {
	glog.Infof("connection pair reuse: id=%d, old_client=%s, new_client=%s", p.RemoteConn.ID(), p.RemoteConn.RemoteAddr(), scon.RemoteAddr())
	p.RemoteConn.SetConn(scon)
}

func (p *ConnPair) Pump() {
	glog.Infof("connection pair new: id=%d, client=%s->%s, server=%s->%s", p.RemoteConn.ID(), p.RemoteConn.RemoteAddr(),
		p.RemoteConn.LocalAddr(), p.LocalConn.LocalAddr(), p.LocalConn.RemoteAddr())
	downloadCh := make(chan int)
	uploadCh := make(chan int)

	go copyUntilError("c2s", p.LocalConn, p.RemoteConn, downloadCh)
	go copyUntilError("s2c", p.RemoteConn, p.LocalConn, uploadCh)

	dlData := <-downloadCh
	dlPackets := <-downloadCh
	ulData := <-uploadCh
	ulPackets := <-uploadCh
	glog.Infof("connection pair remove: id=%d, client=%s, server=%s, down_bytes=%d, down_packets=%d), up_bytes=%d, up_packets=%d", p.RemoteConn.ID(),
		p.RemoteConn.RemoteAddr(), p.LocalConn.LocalAddr(), dlData, dlPackets, ulData, ulPackets)
}

type SCPServer struct {
	idAllocator *scp.IDAllocator

	connPairMutex sync.Mutex
	connPairs     map[int]*ConnPair
}

var defaultServer = &SCPServer{
	idAllocator: scp.NewIDAllocator(1),
	connPairs:   make(map[int]*ConnPair),
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
		glog.Errorf("connection pair id conflict: id=%d", id)
		panic(id)
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
	connPair.RemoteConn = NewSCPConn(scon, configItemTime("scp.reuse_time"))

	// hold conn pair for reuse
	ss.AddConnPair(id, connPair)
	defer ss.RemoveConnPair(id)

	localConn, err := upstream.NewConn(scon)
	if err != nil {
		scon.Close()
		glog.Errorf("upstream new conn failed: client=%s, err=%s", scon.RemoteAddr().String(), err.Error())
		return
	}

	connPair.LocalConn = localConn
	connPair.Pump()
}

func (ss *SCPServer) handleClient(conn net.Conn) {
	defer func() {
		if err := recover(); err != nil {
			glog.Errorf("goroutine failed:%v", err)
			glog.Errorf("stacks: %s", stacks(false))
		}
	}()
	scon := scp.Server(conn, &scp.Config{ScpServer: ss})

	handshakeTimeout := configItemTime("scp.handshake_timeout")
	if handshakeTimeout > 0 {
		scon.SetDeadline(time.Now().Add(handshakeTimeout))
	}
	err := scon.Handshake()
	if handshakeTimeout > 0 {
		scon.SetDeadline(zeroTime)
	}

	if err != nil {
		glog.Errorf("scp handshake faield: client=%s, err=%s", conn.RemoteAddr().String(), err.Error())
		conn.Close()
		return
	}

	if scon.IsReused() {
		ss.onReusedConn(scon)
	} else {
		ss.onNewConn(scon)
	}
}

// Serve accepts incoming connections on the Listener l
func (ss *SCPServer) Serve(l net.Listener) error {
	addr := l.Addr().String()
	glog.Infof("serve: addr=%s", addr)

	var tempDelay time.Duration // how long to sleep on accept failure
	for {
		conn, err := l.Accept()
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
				glog.Errorf("accept connection failed: addr=%s, err=%s, will retry in %v seconds", addr, err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			glog.Errorf("accept failed: addr=%s, err=%s", addr, err.Error())
			return err
		}
		tempDelay = 0
		if glog.V(1) {
			glog.Infof("accept new connection: listen=%s, client=%s", addr, conn.RemoteAddr().String())
		}
		go ss.handleClient(conn)
	}
}
