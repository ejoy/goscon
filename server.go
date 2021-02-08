package main

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
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

func (p *connPair) Pump(serverType string) {
	glog.Infof("scpserver=%s pair new: id=%d, client=%s->%s, server=%s->%s", serverType, p.RemoteConn.ID(), p.RemoteConn.RemoteAddr(),
		p.RemoteConn.LocalAddr(), p.LocalConn.LocalAddr(), p.LocalConn.RemoteAddr())
	downloadCh := make(chan int)
	uploadCh := make(chan int)

	go pump(p.RemoteConn.ID(), serverType+":c2s", p.LocalConn, p.RemoteConn, downloadCh)
	go pump(p.RemoteConn.ID(), serverType+":s2c", p.RemoteConn, p.LocalConn, uploadCh)

	dlData := <-downloadCh
	dlPackets := <-downloadCh
	ulData := <-uploadCh
	ulPackets := <-uploadCh
	glog.Infof("scpserver=%s pair remove: id=%d, client=%s, server=%s, c2s=%d/%d, s2c=%d/%d", serverType, p.RemoteConn.ID(),
		p.RemoteConn.RemoteAddr(), p.LocalConn.LocalAddr(), dlData, dlPackets, ulData, ulPackets)
}

// SCPServer implements scp.SCPServer
type SCPServer struct {
	typ    string // server type
	option atomic.Value
	wg     sync.WaitGroup

	idAllocator *scp.IDAllocator

	// *scp.LoopBufferPool
	reuseBufferPool atomic.Value

	// listener -> scp server -> upstreams
	// support tcp/kcp listener

	tcpListener  *TCPListener
	kcpListeners []*KCPListener // reuseport
	upstreams    *upstream.Upstreams

	connPairMutex sync.Mutex
	connPairs     map[int]*connPair
}

var allServers = make(map[string]*SCPServer)

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
		glog.Errorf("pair id conflict: server=%s, id=%d", ss.typ, id)
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

		glog.Errorf("server=%s pair reuse failed: id=%d, new_client=%s, err=no pair", ss.typ, id, scon.RemoteAddr())
		return false
	}

	oldClientAddr := pair.RemoteConn.RemoteAddr()
	if !pair.RemoteConn.ReplaceConn(scon) {
		scon.Close()
		connectionReuseFails.Inc()

		glog.Errorf("server=%s pair reuse failed: id=%d, old_client=%s, new_client=%s, err=closed", ss.typ, id, oldClientAddr, scon.RemoteAddr())
		return false
	}

	glog.Infof("server=%s pair reuse: id=%d, old_client=%s, new_client=%s", ss.typ, id, oldClientAddr, scon.RemoteAddr())

	connectionReuses.Inc()
	return true
}

func (ss *SCPServer) newUpstreamConn(scon *scp.Conn) (conn net.Conn, err error) {
	localconn, err := ss.upstreams.NewConn(scon)
	if err != nil {
		return
	}
	if scon, ok := localconn.(*scp.Conn); ok {
		option := ss.option.Load().(*Option)
		conn = NewLocalSCPConn(scon, option.SCPOption)
	} else {
		conn = localconn
	}
	return
}

func (ss *SCPServer) onNewConn(scon *scp.Conn) bool {
	id := scon.ID()
	defer ss.ReleaseID(id)

	option := ss.option.Load().(*Option)
	connPair := &connPair{}
	connPair.RemoteConn = NewSCPConn(scon, option.SCPOption)

	// hold conn pair for reuse
	ss.addConnPair(id, connPair)
	defer ss.removeConnPair(id)

	localConn, err := ss.newUpstreamConn(scon)
	if err != nil {
		scon.Close()
		upstreamErrors.Inc()

		glog.Errorf("server=%s upstream new conn failed: id=%d, client=%s, err=%s", ss.typ, id, scon.RemoteAddr(), err.Error())
		return false
	}

	connPair.LocalConn = localConn
	connPair.Pump(ss.typ)
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

	option := ss.option.Load().(*Option)
	scon := scp.Server(conn, &scp.Config{
		ScpServer:        ss,
		ReuseBufferSize:  option.SCPOption.ReuseBuffer,
		HandshakeTimeout: option.SCPOption.HandshakeTimeout * time.Second,
		ReuseBufferPool:  ss.reuseBufferPool.Load().(*scp.LoopBufferPool),
	})
	err := scon.Handshake()

	if err != nil {
		glog.Errorf("scp handshake faield: server=%s, client=%s, err=%s", ss.typ, conn.RemoteAddr().String(), err.Error())
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
func (ss *SCPServer) serve(l net.Listener) error {
	addr := l.Addr().String()
	glog.Infof("server=%s serve: addr=%s", ss.typ, addr)

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
				glog.Errorf("server=%s accept connection failed: addr=%s, err=%s, will retry in %v seconds", ss.typ, addr, err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			glog.Errorf("server=%s accept failed: addr=%s, err=%s", ss.typ, addr, err.Error())
			return err
		}
		tempDelay = 0
		if glog.V(1) {
			glog.Infof("server=%s accept new connection: client=%s", ss.typ, conn.RemoteAddr())
		}

		go ss.handleConn(conn)
	}
}

func (ss *SCPServer) listenTCP(listen string, option *TCPOption) error {
	l, err := NewTCPListener(listen, option)
	if err != nil {
		return fmt.Errorf("listen tcp failed: addr=%s, err=%s", listen, err.Error())
	}
	glog.Infof("server=%s start listen tcp: addr=%s", ss.typ, listen)

	ss.wg.Add(1)
	ss.tcpListener = l
	go func(l net.Listener) {
		defer l.Close()
		defer ss.wg.Done()
		err := ss.serve(l)
		glog.Errorf("server=%s stop listen tcp: addr=%s, err=%s", ss.typ, listen, err.Error())
	}(l)
	return nil
}

func (ss *SCPServer) listenKCP(listen string, option *KCPOption) error {
	reuseport := option.ReusePort
	if reuseport <= 0 {
		reuseport = 1
	}
	for i := 0; i < reuseport; i++ {
		l, err := NewKCPListener(listen, option)
		if err != nil {
			return fmt.Errorf("listen kcp failed: addr=%s, err=%s", listen, err.Error())
		}
		glog.Infof("server=%s start listen kcp: addr=%s", ss.typ, listen)

		ss.wg.Add(1)
		ss.kcpListeners = append(ss.kcpListeners, l)
		go func(l net.Listener) {
			defer l.Close()
			defer ss.wg.Done()
			err := ss.serve(l)
			glog.Errorf("server=%s stop listen kcp: addr=%s, err=%s", ss.typ, listen, err.Error())
		}(l)
	}
	return nil
}

// Listen listens scp over tcp or kcp.
func (ss *SCPServer) Listen() error {
	option := ss.option.Load().(*Option)
	switch option.Net {
	case "", "tcp":
		if err := ss.listenTCP(option.Listen, option.TCPOption); err != nil {
			return err
		}
		break
	case "kcp":
		if err := ss.listenKCP(option.Listen, option.KCPOption); err != nil {
			return err
		}
		break
	}
	return nil
}

// Done returns when all listeners closed.
func (ss *SCPServer) Done() {
	ss.wg.Wait()
}

func newReuseBufferPool(cap int) *scp.LoopBufferPool {
	return &scp.LoopBufferPool{
		Cap: cap,
		Pool: sync.Pool{
			New: func() interface{} {
				return scp.NewLoopBuffer(cap)
			},
		},
	}
}

func reloadAllServers() {
	for typ, ss := range allServers {
		option := GetConfigServerOption(typ)
		if option != nil {
			if err := ss.upstreams.SetOption(option.Upstream); err == nil {
				ss.tcpListener.SetOption(option.TCPOption)
				for _, kcpListener := range ss.kcpListeners {
					kcpListener.SetOption(option.KCPOption)
				}
				glog.Infof("reload server=%s options", typ)
			} else {
				glog.Errorf("reload server=%s options failed", typ)
			}

			// ensure no error below.
			ss.option.Store(option)
			oldReuseBufferPool := ss.reuseBufferPool.Load().(*scp.LoopBufferPool)
			if oldReuseBufferPool.Cap != option.SCPOption.ReuseBuffer {
				reuseBufferPool := newReuseBufferPool(option.SCPOption.ReuseBuffer)
				ss.reuseBufferPool.Store(reuseBufferPool)
			}
		}
	}
}

func startServer(typ string, option *Option) error {
	u, err := upstream.New(option.Upstream)
	if err != nil {
		return fmt.Errorf("start server=%s upstream failed, err=%s", typ, err.Error())
	}

	reuseBufferPool := newReuseBufferPool(option.SCPOption.ReuseBuffer)
	ss := &SCPServer{
		typ:         typ,
		idAllocator: scp.NewIDAllocator(1),
		connPairs:   make(map[int]*connPair),
		upstreams:   u,
	}
	ss.reuseBufferPool.Store(reuseBufferPool)
	ss.option.Store(option)
	allServers[typ] = ss
	if err := ss.Listen(); err != nil {
		return fmt.Errorf("start server=%s listen failed, err=%s", typ, err.Error())
	}
	return nil
}

func allServerDone() {
	for _, ss := range allServers {
		ss.Done()
	}
}
