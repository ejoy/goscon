package main

import (
	"net"
	"sync"
	"time"

	"github.com/ejoy/goscon/scp"
	"github.com/ejoy/goscon/upstream"
	"github.com/xjdrew/glog"
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

func pump(id int, tag string, dst net.Conn, src net.Conn, ch chan<- int) error {
	var err error
	var written, packets int
	buf := copyPool.Get().([]byte)
	defer copyPool.Put(buf)

	for {
		nr, er := src.Read(buf)
		if glog.V(2) {
			glog.Infof("recv packet: id=%d, tag=%s, addr=%s, sz=%d, err=%v", id, tag, src.RemoteAddr(), nr, er)
		}
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if glog.V(2) {
				glog.Infof("send packet: id=%d, tag=%s, addr=%s, sz=%d, err=%v", id, tag, dst.RemoteAddr(), nw, ew)
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
		glog.Infof("pair pump: id=%d, tag=%s, addr1=%s, addr2=%s, err=%v", id, tag, src.RemoteAddr(), dst.RemoteAddr(), err)
	}

	src.Close()
	dst.Close()

	ch <- written
	ch <- packets
	return err
}

func (p *connPair) Pump() {
	glog.Infof("pair new: id=%d, client=%s->%s, server=%s->%s", p.RemoteConn.ID(), p.RemoteConn.RemoteAddr(),
		p.RemoteConn.LocalAddr(), p.LocalConn.LocalAddr(), p.LocalConn.RemoteAddr())
	downloadCh := make(chan int)
	uploadCh := make(chan int)

	go pump(p.RemoteConn.ID(), "c2s", p.LocalConn, p.RemoteConn, downloadCh)
	go pump(p.RemoteConn.ID(), "s2c", p.RemoteConn, p.LocalConn, uploadCh)

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
		return pair.RemoteConn.Conn
	}
	return nil
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

	connectionResend.Observe(float64(scon.ReuseState()))

	if pair == nil {
		scon.Close()
		connectionReuseFails.Inc()

		glog.Errorf("pair reuse failed: id=%d, new_client=%s, err=no pair", id, scon.RemoteAddr())
		return false
	}

	oldClientAddr := pair.RemoteConn.RemoteAddr()
	if !pair.RemoteConn.ReplaceConn(scon) {
		scon.Close()
		connectionReuseFails.Inc()

		glog.Errorf("pair reuse failed: id=%d, old_client=%s, new_client=%s, err=closed", id, oldClientAddr, scon.RemoteAddr())
		return false
	}

	glog.Infof("pair reuse: id=%d, old_client=%s, new_client=%s", id, oldClientAddr, scon.RemoteAddr())

	connectionReuses.Inc()
	return true
}

func newUpstreamConn(scon *scp.Conn) (conn net.Conn, err error) {
	localconn, err := upstream.NewConn(scon)
	if err != nil {
		return
	}
	if scon, ok := localconn.(*scp.Conn); ok {
		conn = NewSCPConn(scon)
	} else {
		conn = localconn
	}
	return
}

func (ss *SCPServer) onNewConn(scon *scp.Conn) bool {
	id := scon.ID()
	defer ss.ReleaseID(id)

	connPair := &connPair{}
	connPair.RemoteConn = NewSCPConn(scon)

	// hold conn pair for reuse
	ss.addConnPair(id, connPair)
	defer ss.removeConnPair(id)

	localConn, err := newUpstreamConn(scon)
	if err != nil {
		scon.Close()
		upstreamErrors.Inc()

		glog.Errorf("upstream new conn failed: id=%d, client=%s, err=%s", id, scon.RemoteAddr(), err.Error())
		return false
	}

	connPair.LocalConn = localConn
	connPair.Pump()
	return true
}

func (ss *SCPServer) handleConn(conn net.Conn) {
	connectionAccepts.Inc()

	defer func() {
		connectionCloses.Inc()
		if err := recover(); err != nil {
			glog.Errorf("goroutine failed:%v", err)
			glog.Errorf("stacks: %s", stacks(false))
		}
	}()

	scon := scp.Server(conn, &scp.Config{ScpServer: ss})

	err := scon.Handshake()

	if err != nil {
		glog.Errorf("scp handshake faield: client=%s, err=%s", conn.RemoteAddr().String(), err.Error())
		scon.Close()
		metricOnHandshakeError(err)
		return
	}

	if scon.IsReused() {
		ss.onReuseConn(scon)
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
			connectionAcceptFails.Inc()

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
			glog.Infof("accept new connection: client=%s", conn.RemoteAddr())
		}

		go ss.handleConn(conn)
	}
}
