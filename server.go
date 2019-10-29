package main

import (
	"net"
	"sync"
	"time"

	"github.com/ejoy/goscon/scp"
	"github.com/ejoy/goscon/upstream"
	"github.com/golang/glog"
)

var copyPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, scp.NetBufferSize)
	},
}

type connPair struct {
	LocalConn  net.Conn // scp server <-> local server
	RemoteConn *SCPConn // client <-> scp server
}

func copyUntilError(tag string, dst net.Conn, src net.Conn, ch chan<- int) error {
	var err error
	var written, packets int
	buf := copyPool.Get().([]byte)
	defer copyPool.Put(buf)

	for {
		nr, er := src.Read(buf)
		if glog.V(2) {
			glog.Infof("recv packet: tag=%s, addr=%s, sz=%d", tag, src.RemoteAddr(), nr)
		}
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if glog.V(2) {
				glog.Infof("send packet: tag=%s, addr=%s, sz=%d", tag, dst.RemoteAddr(), nw)
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

func (p *connPair) Pump() {
	glog.Infof("pair new: id=%d, client=%s->%s, server=%s->%s", p.RemoteConn.ID(), p.RemoteConn.RemoteAddr(),
		p.RemoteConn.LocalAddr(), p.LocalConn.LocalAddr(), p.LocalConn.RemoteAddr())
	downloadCh := make(chan int)
	uploadCh := make(chan int)

	go copyUntilError("c2s", p.LocalConn, p.RemoteConn, downloadCh)
	go copyUntilError("s2c", p.RemoteConn, p.LocalConn, uploadCh)

	dlData := <-downloadCh
	dlPackets := <-downloadCh
	ulData := <-uploadCh
	ulPackets := <-uploadCh
	glog.Infof("pair remove: id=%d, client=%s, server=%s, c2s=%d/%d, s2c=%d/%d", p.RemoteConn.ID(),
		p.RemoteConn.RemoteAddr(), p.LocalConn.LocalAddr(), dlData, dlPackets, ulData, ulPackets)
}

// SCPServer implements scp.SCPServer
type SCPServer struct {
	idAllocator *scp.IDAllocator

	connPairMutex sync.Mutex
	connPairs     map[int]*connPair
}

var defaultServer = &SCPServer{
	idAllocator: scp.NewIDAllocator(1),
	connPairs:   make(map[int]*connPair),
}

// AcquireID implments scp.SCPServer interface
func (ss *SCPServer) AcquireID() int {
	return ss.idAllocator.AcquireID()
}

// ReleaseID implments scp.SCPServer interface
func (ss *SCPServer) ReleaseID(id int) {
	ss.idAllocator.ReleaseID(id)
}

// QueryByID implments scp.SCPServer interface
func (ss *SCPServer) QueryByID(id int) *scp.Conn {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	pair := ss.connPairs[id]
	if pair != nil {
		return pair.RemoteConn.RawConn()
	}
	return nil
}

// NumOfConnPairs .
func (ss *SCPServer) NumOfConnPairs() int {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	return len(ss.connPairs)
}

func (ss *SCPServer) addConnPair(id int, pair *connPair) {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	if _, ok := ss.connPairs[id]; ok {
		glog.Errorf("pair id conflict: id=%d", id)
		panic(id)
	}
	ss.connPairs[id] = pair
}

func (ss *SCPServer) removeConnPair(id int) {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	delete(ss.connPairs, id)
}

func (ss *SCPServer) getConnPair(id int) *connPair {
	ss.connPairMutex.Lock()
	defer ss.connPairMutex.Unlock()
	return ss.connPairs[id]
}

func (ss *SCPServer) onReuseConn(scon *scp.Conn) bool {
	id := scon.ID()
	pair := ss.getConnPair(id)

	if pair == nil {
		glog.Errorf("pair reuse failed: id=%d, new_client=%s, err=no pair", id, scon.RemoteAddr())
		return false
	}
	oldClientAddr := pair.RemoteConn.RemoteAddr()
	if !pair.RemoteConn.SetConn(scon) {
		glog.Errorf("pair reuse failed: id=%d, old_client=%s, new_client=%s, err=closed", id, oldClientAddr, scon.RemoteAddr())
	}

	glog.Infof("pair reuse: id=%d, old_client=%s, new_client=%s", id, oldClientAddr, scon.RemoteAddr())
	return true
}

func (ss *SCPServer) onNewConn(scon *scp.Conn) bool {
	id := scon.ID()
	defer ss.ReleaseID(id)

	connPair := &connPair{}
	connPair.RemoteConn = NewSCPConn(scon)

	// hold conn pair for reuse
	ss.addConnPair(id, connPair)
	defer ss.removeConnPair(id)

	localConn, err := upstream.NewConn(scon)
	if err != nil {
		glog.Errorf("upstream new conn failed: id=%d, client=%s, err=%s", id, scon.RemoteAddr(), err.Error())
		return false
	}

	connPair.LocalConn = localConn
	connPair.Pump()
	return true
}

func (ss *SCPServer) handleConn(conn net.Conn) {
	defer func() {
		if err := recover(); err != nil {
			glog.Errorf("goroutine failed:%v", err)
			glog.Errorf("stacks: %s", stacks(false))
		}
	}()

	scon := scp.Server(conn, &scp.Config{ScpServer: ss})

	// handshake
	handshakeTimeout := configItemTime("scp.handshake_timeout")
	if handshakeTimeout > 0 {
		scon.SetDeadline(time.Now().Add(handshakeTimeout))
		defer scon.SetDeadline(zeroTime)
	}

	if err := scon.Handshake(); err != nil {
		glog.Errorf("scp handshake faield: client=%s, err=%s", conn.RemoteAddr().String(), err.Error())
		scon.Close()
		return
	}

	var ok bool
	if scon.IsReused() {
		ok = ss.onReuseConn(scon)
	} else {
		ok = ss.onNewConn(scon)
	}
	if !ok {
		scon.Close()
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
		go ss.handleConn(conn)
	}
}
