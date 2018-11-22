//
//   date  : 2014-05-23 17:35
//   author: xjdrew
//

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/ejoy/goscon/scp"
)

var errNoHost = errors.New("no host")

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [config]\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

type Host struct {
	Addr   string `json:"addr"`
	Weight int    `json:"weight"`
	Name   string `json:"name"`

	addr *net.TCPAddr
}

type Config struct {
	Hosts []Host `json:"hosts"`
}

type LocalConnWrapper interface {
	Wrapper(local *net.TCPConn, remote net.Conn) (*net.TCPConn, error)
}

type LocalConnProvider struct {
	sync.Mutex
	hosts  []Host
	weight int

	wrapper LocalConnWrapper

	ConfigFile string
}

func (tp *LocalConnProvider) MustSetWrapper(wrapper LocalConnWrapper) {
	if tp.wrapper != nil {
		panic("tp.wrapper != nil")
	}
	tp.wrapper = wrapper
}

func (tp *LocalConnProvider) GetHostByWeight() *Host {
	v := rand.Intn(tp.weight)
	for _, host := range tp.hosts {
		if host.Weight >= v {
			return &host
		}
		v -= host.Weight
	}
	return nil
}

func (tp *LocalConnProvider) GetHostByName(name string) *Host {
	for _, host := range tp.hosts {
		if host.Name == name {
			return &host
		}
	}
	Log("GetHostByName failed: %s", name)
	return nil
}

func (tp *LocalConnProvider) GetHost(preferred string) *Host {
	if preferred == "" {
		return tp.GetHostByWeight()
	} else {
		return tp.GetHostByName(preferred)
	}
}

func (tp *LocalConnProvider) CreateLocalConn(remoteConn *scp.Conn) (*net.TCPConn, error) {
	host := glbLocalConnProvider.GetHost(remoteConn.TargetServer())
	if host == nil {
		return nil, errNoHost
	}

	conn, err := net.DialTCP("tcp", nil, host.addr)
	if err != nil {
		return nil, err
	}

	if tp.wrapper == nil {
		return conn, err
	}

	newConn, err := tp.wrapper.Wrapper(conn, remoteConn)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return newConn, nil
}

func (tp *LocalConnProvider) reset(hosts []Host) error {
	var weight int
	for i := range hosts {
		host := &hosts[i]
		if addr, err := net.ResolveTCPAddr("tcp", host.Addr); err != nil {
			return err
		} else {
			host.addr = addr
		}
		weight += host.Weight
	}

	if weight <= 0 {
		return fmt.Errorf("no hosts")
	}

	tp.Lock()
	tp.hosts = hosts
	tp.weight = weight
	tp.Unlock()
	return nil
}

func (tp *LocalConnProvider) Reload() error {
	fp, err := os.Open(tp.ConfigFile)
	if err != nil {
		return err
	}
	defer fp.Close()

	var config Config
	dec := json.NewDecoder(fp)
	err = dec.Decode(&config)
	if err != nil {
		return err
	}

	return tp.reset(config.Hosts)
}

const SIG_RELOAD = syscall.Signal(34)
const SIG_STATUS = syscall.Signal(35)

func reload() {
	err := glbLocalConnProvider.Reload()
	if err != nil {
		Log("reload failed: %s", err.Error())
		return
	}
	Log("reload succeed")
}

func status() {
	Log("status:\n\t"+
		"procs:%d/%d\n\t"+
		"goroutines:%d\n\t"+
		"actives:%d",
		runtime.GOMAXPROCS(0), runtime.NumCPU(),
		runtime.NumGoroutine(),
		glbScpServer.NumOfConnPairs())
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
			Log("catch sigterm, ignore")
		}
	}
}

var glbScpServer *SCPServer
var glbLocalConnProvider *LocalConnProvider

var optUploadMinPacket, optUploadMaxDelay int

var glbWrapperHooks []func(provider *LocalConnProvider)

func installWrapperHook(hook func(provider *LocalConnProvider)) {
	glbWrapperHooks = append(glbWrapperHooks, hook)
}

func wrapperHook(provider *LocalConnProvider) {
	for _, hook := range glbWrapperHooks {
		hook(provider)
	}
}

func main() {
	// deal with arguments
	var config string
	var listen string
	var reuseTimeout int
	var sentCacheSize int

	// kcp options
	var fecData int
	var fecParity int

	flag.StringVar(&config, "config", "./settings.conf", "backend servers config file")
	flag.StringVar(&listen, "listen", "0.0.0.0:1248", "local listen port(0.0.0.0:1248)")
	flag.IntVar(&logLevel, "log", 2, "larger value for detail log")
	flag.IntVar(&reuseTimeout, "timeout", 30, "reuse timeout")
	flag.IntVar(&sentCacheSize, "sbuf", 65536, "sent cache size")
	flag.IntVar(&optUploadMinPacket, "uploadMinPacket", 0, "upload minimal packet")
	flag.IntVar(&optUploadMaxDelay, "uploadMaxDelay", 0, "upload maximal delay milliseconds")

	// tcp := flag.NewFlagSet("tcp", flag.ExitOnError)

	kcp := flag.NewFlagSet("kcp", flag.ExitOnError)
	kcp.IntVar(&fecData, "fec_data", 1, "FEC: number of shards to split the data into")
	kcp.IntVar(&fecParity, "fec_parity", 0, "FEC: number of parity shards")

	flag.Usage = usage
	flag.Parse()

	glbLocalConnProvider = new(LocalConnProvider)
	glbLocalConnProvider.ConfigFile = config
	Info("config file: %s", glbLocalConnProvider.ConfigFile)

	if err := glbLocalConnProvider.Reload(); err != nil {
		Error("load target pool failed: %s", err.Error())
		return
	}

	wrapperHook(glbLocalConnProvider)

	if sentCacheSize > 0 {
		scp.SentCacheSize = sentCacheSize
	}

	go handleSignal()

	options := &Options{
		timeout: reuseTimeout,
	}

	var network string
	args := flag.Args()

	if len(args) > 0 && args[0] == "kcp" {
		kcp.Parse(args[1:])
		options.fecData = fecData
		options.fecParity = fecParity
		network = "kcp"
	} else {
		network = "tcp"
	}
	glbScpServer = NewSCPServer(options)

	Log("options: %v", options)
	glbScpServer.Start(network, listen)
}
