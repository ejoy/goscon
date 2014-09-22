//
//   date  : 2014-05-23 17:35
//   author: xjdrew
//

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [config]\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

type Options struct {
	ConfigFile string
	LocalAddr  string
	LogLevel   uint
	Timeout    uint
	SendBuf    uint
	MaxProcs   int
}

type Host struct {
	Addr   string
	Weight int

	addr *net.TCPAddr
}

type Settings struct {
	Hosts  []Host
	weight int
}

type Status struct {
	actives int32
}

type ReuseConn struct {
	conn *net.TCPConn
	req  *ReuseConnReq
}

type ReuseError struct {
	rc   *ReuseConn
	code uint32
}

type Daemon struct {
	// host list
	settings *Settings

	// listen port
	ln     *net.TCPListener
	wg     sync.WaitGroup
	status Status

	// next linkid
	nextidCh chan uint32

	// links
	links map[uint32]*StableLink

	// event channel
	// *StableLink new conn
	// *ReuseConn reuse conn
	eventCh chan interface{}
	errCh   chan *ReuseError
}

var options Options
var daemon Daemon

func readSettings(config_file string) *Settings {
	fp, err := os.Open(config_file)
	if err != nil {
		Error("open config file failed:%s", err.Error())
		return nil
	}
	defer fp.Close()

	var settings Settings
	dec := json.NewDecoder(fp)
	err = dec.Decode(&settings)
	if err != nil {
		Error("decode config file failed:%s", err.Error())
		return nil
	}

	for i := range settings.Hosts {
		host := &settings.Hosts[i]
		host.addr, err = net.ResolveTCPAddr("tcp", host.Addr)
		if err != nil {
			Error("resolve local addr failed:%s", err.Error())
			return nil
		}
		settings.weight += host.Weight
	}

	Info("config:%v", settings)
	return &settings
}

func chooseHost(weight int, hosts []Host) *Host {
	if weight <= 0 {
		return nil
	}

	v := rand.Intn(weight)
	for _, host := range hosts {
		if host.Weight >= v {
			return &host
		}
		v -= host.Weight
	}
	return nil
}

func onNewConn(source *net.TCPConn, req *NewConnReq) {
	settings := daemon.settings
	host := chooseHost(settings.weight, settings.Hosts)
	if host == nil {
		source.Close()
		Error("choose host failed:%v", source.RemoteAddr())
		return
	}

	dest, err := net.DialTCP("tcp", nil, host.addr)
	if err != nil {
		source.Close()
		Error("connect to %s failed: %s", host.addr, err.Error())
		return
	}

	dest.SetKeepAlive(true)
	dest.SetKeepAlivePeriod(time.Second * 60)
	dest.SetLinger(-1)

	id := <-daemon.nextidCh
	link := NewStableLink(id, source, dest, req.key)
	daemon.eventCh <- link
	link.Run()
	daemon.eventCh <- link
	link.Wait()
}

func onReuseConn(source *net.TCPConn, req *ReuseConnReq) {
	daemon.eventCh <- &ReuseConn{source, req}
}

func handleClient(source *net.TCPConn) {
	atomic.AddInt32(&daemon.status.actives, 1)
	defer func() {
		atomic.AddInt32(&daemon.status.actives, -1)
		daemon.wg.Done()
	}()

	Info("accept new connection: %v", source.RemoteAddr())

	source.SetKeepAlive(true)
	source.SetKeepAlivePeriod(time.Second * 60)
	source.SetLinger(-1)

	// read req
	// set read request timeout
	source.SetReadDeadline(time.Now().Add(time.Second * 30))
	err, req := ReadReq(source)
	if err != nil {
		source.Close()
		Error("conn:%v, read req failed: %v", source.RemoteAddr(), err)
		return
	}

	// cancel read timeout
	var t time.Time
	source.SetReadDeadline(t)

	// judge: new conn or reuse conn
	switch req := req.(type) {
	case *NewConnReq:
		Info("new conn request:%v", req)
		onNewConn(source, req)
	case *ReuseConnReq:
		Info("reuse conn request:%v", req)
		onReuseConn(source, req)
	default:
		Info("unknown request:%v", req)
		source.Close()
		return
	}
	Info("connection close: %v", source.RemoteAddr())
}

func onEventLink(link *StableLink) {
	if !link.IsBroken() {
		daemon.links[link.id] = link
	} else {
		link.StopReuse()
		delete(daemon.links, link.id)
		daemon.nextidCh <- link.id
	}
}

func onEventReuse(rc *ReuseConn) {
	link := daemon.links[rc.req.id]
	var code uint32
	if link == nil {
		code = 404
	} else {
		code = link.VerifyReuse(rc.req)
	}
	if code == 200 {
		link.Reuse(rc)
	} else {
		daemon.errCh <- &ReuseError{rc, code}
	}
}

func dispatch() {
	for {
		event := <-daemon.eventCh
		switch event := event.(type) {
		case *StableLink:
			onEventLink(event)
		case *ReuseConn:
			onEventReuse(event)
		}
	}
}

func dispatchErr() {
	for {
		e, ok := <-daemon.errCh
		if !ok {
			break
		}
		Info("link(%d) reuse failed:%d", e.rc.req.id, e.code)
		e.rc.conn.SetWriteDeadline(time.Now().Add(time.Second))
		WriteReuseConnResp(e.rc.conn, 0, e.code)
		e.rc.conn.Close()
	}
}

func start() {
	daemon.wg.Add(1)
	defer func() {
		daemon.wg.Done()
	}()

	laddr, err := net.ResolveTCPAddr("tcp", options.LocalAddr)
	if err != nil {
		Error("resolve local addr failed:%s", err.Error())
		return
	}

	ln, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		Error("build listener failed:%s", err.Error())
		return
	}

	daemon.ln = ln
	go dispatch()
	go dispatchErr()
	for {
		conn, err := daemon.ln.AcceptTCP()
		if err != nil {
			Error("accept failed:%s", err.Error())
			continue
		}
		daemon.wg.Add(1)
		go handleClient(conn)
	}
}

const SIG_RELOAD = syscall.Signal(34)
const SIG_STATUS = syscall.Signal(35)

func reload() {
	settings := readSettings(options.ConfigFile)
	if settings == nil {
		Info("reload failed")
		return
	}
	daemon.settings = settings
	Info("reload succeed")
}

func status() {
	Info("status:\n\t"+
		"procs:%d/%d\n\t"+
		"goroutines:%d\n\t"+
		"actives:%d",
		runtime.GOMAXPROCS(0), runtime.NumCPU(),
		runtime.NumGoroutine(),
		daemon.status.actives)
}

func handleSignal() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, SIG_RELOAD, SIG_STATUS, syscall.SIGTERM)

	for sig := range c {
		switch sig {
		case SIG_RELOAD:
			reload()
		case SIG_STATUS:
			status()
		case syscall.SIGTERM:
			Info("catch sigterm, ignore")
		}
	}
}

func argsCheck() {
	flag.StringVar(&options.LocalAddr, "listen_addr", "0.0.0.0:1248", "local listen port(0.0.0.0:1248)")
	flag.UintVar(&options.LogLevel, "log", 3, "larger value for detail log")
	flag.UintVar(&options.Timeout, "timeout", 30, "reuse timeout")
	flag.UintVar(&options.SendBuf, "sbuf", 16384, "send buffer")
	flag.IntVar(&options.MaxProcs, "procs", 2, "max procs")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		Error("config file is missing.")
		os.Exit(1)
	}

	options.ConfigFile = args[0]
	Info("config file is: %s", options.ConfigFile)

	if options.MaxProcs > 0 {
		Info("set max procs:%d -> %d", runtime.GOMAXPROCS(options.MaxProcs), options.MaxProcs)
	}
}

func main() {
	// deal with arguments
	argsCheck()

	// init daemon
	daemon.settings = readSettings(options.ConfigFile)
	if daemon.settings == nil {
		Error("parse config failed")
		os.Exit(1)
	}

	var sz uint32 = 32767
	daemon.nextidCh = make(chan uint32, sz)
	var i uint32
	for i = 1; i <= sz; i++ {
		daemon.nextidCh <- i
	}

	daemon.links = make(map[uint32]*StableLink)
	daemon.eventCh = make(chan interface{})
	daemon.errCh = make(chan *ReuseError, 1024)

	// run
	Info("goscon started")
	go handleSignal()
	start()
	daemon.wg.Wait()
}
