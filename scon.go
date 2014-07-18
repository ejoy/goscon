//
//   date  : 2014-05-23 17:35
//   author: xjdrew
//

package main

import (
	"flag"
	"fmt"
	"log"
	"log/syslog"

	"encoding/json"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"net"
	"sync"
	"sync/atomic"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [config]\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
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

type StableLink struct {
	id uint32

	// build
	secret uint64
	index  uint32

	// conn pair
	remote  *net.TCPConn
	local   *net.TCPConn
	reuseCh chan bool

	// data
	receiveLock sync.Mutex
	received    uint32

	sendLock sync.Mutex
	sent     uint32

	used  int
	cache []byte

	//
	errTime time.Time
}

type Daemon struct {
	// host list
	config_file string
	settings    *Settings

	// listen port
	localAddr string

	// listen port
	ln     *net.TCPListener
	wg     sync.WaitGroup
	status Status

	// next linkid
	nextid uint32

	// connections
	linkRW   sync.RWMutex
	linksMap map[uint32]*StableLink
}

var daemon Daemon
var logger *log.Logger

func readSettings(config_file string) *Settings {
	fp, err := os.Open(config_file)
	if err != nil {
		logger.Printf("open config file failed:%s", err.Error())
		return nil
	}
	defer fp.Close()

	var settings Settings
	dec := json.NewDecoder(fp)
	err = dec.Decode(&settings)
	if err != nil {
		logger.Printf("decode config file failed:%s", err.Error())
		return nil
	}

	for i := range settings.Hosts {
		host := &settings.Hosts[i]
		host.addr, err = net.ResolveTCPAddr("tcp", host.Addr)
		if err != nil {
			logger.Printf("resolve local addr failed:%s", err.Error())
			return nil
		}
		settings.weight += host.Weight
	}

	logger.Printf("config:%v", settings)
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

func getStableLink(id uint32) *StableLink {
	daemon.linkRW.RLock()
	defer daemon.linkRW.RUnlock()
	return daemon.linksMap[id]
}

func delStableLink(id uint32) {
	daemon.linkRW.Lock()
	delete(daemon.linksMap, id)
	daemon.linkRW.Unlock()
}

func newStableLink() *StableLink {
	link := new(StableLink)

	daemon.linkRW.Lock()
	for {
		nextid := atomic.AddUint32(&daemon.nextid, 1)
		if daemon.linksMap[nextid] == nil {
			link.id = nextid
			daemon.linksMap[nextid] = link
			break
		}
	}
	daemon.linkRW.Unlock()

	link.used = 0
	link.cache = make([]byte, 0x1000)
	return link
}

func forwardToRemote(link *StableLink) {
	remote, local := link.remote, link.local

	bufsz := 1 << 12
	cache := make([]byte, bufsz)
	for {
		// pump from local
		n, err := local.Read(cache)
		if err != nil {
			// local error, shoud close link
			break
		}

		link.sendLock.Lock()
		// cache last sent
		link.sent += uint32(n)
		if link.used+n > cap(link.cache) {
			link.used = cap(link.cache) - n
			copy(link.cache, link.cache[:link.used])
		}
		copy(link.cache[link.used:], cache[:n])
		link.used += n
		link.sendLock.Unlock()

		// pour into remote
		err = WriteAll(remote, cache[:n])
		if err != nil {
			<-link.reuseCh
			remote = link.remote
			// resend
			continue
		}
	}
}

func forwardToLocal(link *StableLink) {
	link.receiveLock.Lock()
	remote, local := link.remote, link.local

	bufsz := 1 << 12
	cache := make([]byte, bufsz)
	for {
		// pump from remote
		n, err := remote.Read(cache)
		if err != nil {
			// remote error
			link.receiveLock.Unlock()
			<-link.reuseCh
			remote = link.remote
			link.receiveLock.Lock()
			continue
		}

		link.received += uint32(n)

		// pour into local
		err = WriteAll(local, cache[:n])
		if err != nil {
			// local error, shoud close link
			break
		}
	}
}

func onNewConn(source *net.TCPConn, req *NewConnReq) {
	settings := daemon.settings
	host := chooseHost(settings.weight, settings.Hosts)
	if host == nil {
		source.Close()
		logger.Println("choose host failed:%v", source.RemoteAddr())
		return
	}

	dest, err := net.DialTCP("tcp", nil, host.addr)
	if err != nil {
		source.Close()
		logger.Printf("connect to %s failed: %s", host.addr, err.Error())
		return
	}

	source.SetKeepAlive(true)
	source.SetKeepAlivePeriod(time.Second * 60)
	source.SetLinger(-1)
	dest.SetLinger(-1)

	token, secret := Gentoken(req.key)

	link := newStableLink()
	link.remote = source
	link.local = dest
	link.secret = secret

	err = WriteNewConnResp(source, link.id, token)
	if err != nil {
		source.Close()
		delStableLink(link.id)
		logger.Printf("write new conn resp to %v failed:%v", source.RemoteAddr(), err.Error())
		return
	}

	go forwardToLocal(link)
	forwardToRemote(link)
}

func onReuseConn(source *net.TCPConn, req *ReuseConnReq) {
	link := getStableLink(req.id)
	if link == nil {
		WriteReuseConnResp(source, 0, 404)
		source.Close()
		return
	}

	if link.index >= req.index {
		WriteReuseConnResp(source, 0, 403)
		source.Close()
		return
	}

	if !VerifySecret(req, link) {
		WriteReuseConnResp(source, 0, 401)
		source.Close()
		return
	}

	// ok, try reuse conn

	// close old conn
	remote := link.remote
	link.remote = nil
	remote.Close()

	//
	link.index = req.index
	// calcuate buff
	link.sendLock.Lock()
	var diff uint32
	if link.sent < req.received {
		diff = link.sent + 0xffffffff - req.received
	} else {
		diff = link.sent - req.received
	}

	if diff > uint32(link.used) {
		WriteReuseConnResp(source, 0, 406)
		source.Close()
		link.sendLock.Unlock()
		return
	}

	link.receiveLock.Lock()
	err := WriteReuseConnResp(source, link.received, 200)
	link.receiveLock.Unlock()
	if err != nil {
		logger.Printf("write reuse conn resp to %v failed:%v", source.RemoteAddr(), err.Error())
		source.Close()
		return
	}

	// resend buffered
	if diff > 0 {
		from := uint32(link.used) - diff
		err = WriteAll(source, link.cache[from:link.used])
		link.sendLock.Unlock()
		if err != nil {
			// remote failed
			return
		}
	}

	// reset
	link.remote = remote
	link.reuseCh <- true
	link.reuseCh <- true
}

func handleClient(source *net.TCPConn) {
	atomic.AddInt32(&daemon.status.actives, 1)
	defer func() {
		atomic.AddInt32(&daemon.status.actives, -1)
		daemon.wg.Done()
	}()

	// read req
	err, req := ReadReq(source)
	if err != nil {
		source.Close()
		logger.Printf("read req failed:%v", err)
		return
	}
	// judge: new conn or reuse conn
	switch req := req.(type) {
	case *NewConnReq:
		fmt.Printf("new conn request:%v", req)
		onNewConn(source, req)
	case *ReuseConnReq:
		fmt.Printf("reuse conn request:%v", req)
		onReuseConn(source, req)
	default:
		fmt.Printf("unknown request:%v", req)
		source.Close()
		return
	}
}

func start() {
	daemon.wg.Add(1)
	defer func() {
		daemon.wg.Done()
	}()

	laddr, err := net.ResolveTCPAddr("tcp", daemon.localAddr)
	if err != nil {
		logger.Printf("resolve local addr failed:%s", err.Error())
		return
	}

	ln, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		logger.Printf("build listener failed:%s", err.Error())
		return
	}

	daemon.ln = ln
	for {
		conn, err := daemon.ln.AcceptTCP()
		if err != nil {
			logger.Printf("accept failed:%s", err.Error())
			break
		}
		daemon.wg.Add(1)
		go handleClient(conn)
	}
}

const SIG_RELOAD = syscall.Signal(34)
const SIG_STATUS = syscall.Signal(35)

func reload() {
	settings := readSettings(daemon.config_file)
	if settings == nil {
		logger.Println("reload failed")
		return
	}
	daemon.settings = settings
	logger.Println("reload succeed")
}

func status() {
	logger.Printf("status: actives-> %d", daemon.status.actives)
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
			logger.Println("catch sigterm, ignore")
		}
	}
}

func init() {
	var err error
	logger, err = syslog.NewLogger(syslog.LOG_LOCAL0, 0)
	if err != nil {
		fmt.Printf("create logger failed:%s", err.Error())
		os.Exit(1)
	}
	logger.Println("are you lucky? go!")
	rand.Seed(time.Now().Unix())
}

func main() {
	flag.StringVar(&daemon.localAddr, "listen_addr", "0.0.0.0:1248", "local listen port(0.0.0.0:1248)")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		logger.Println("config file is missing.")
		os.Exit(1)
	}

	daemon.config_file = args[0]
	logger.Printf("config file is: %s", daemon.config_file)

	daemon.settings = readSettings(daemon.config_file)
	if daemon.settings == nil {
		logger.Println("parse config failed")
		os.Exit(1)
	}

	go handleSignal()

	daemon.linksMap = make(map[uint32]*StableLink)
	daemon.nextid = 1

	// run
	start()
	daemon.wg.Wait()
}
