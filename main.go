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
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/ejoy/goscon/scp"
)

var errNoHost = errors.New("no host")

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [options]\n", os.Args[0])
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

var (
	optProtocol        = 0
	optUploadMinPacket int
	optUploadMaxDelay  int
)

const (
	TCP = 1 << 0
	KCP = 1 << 1
)

var glbWrapperHooks []func(provider *LocalConnProvider)

func installWrapperHook(hook func(provider *LocalConnProvider)) {
	glbWrapperHooks = append(glbWrapperHooks, hook)
}

func wrapperHook(provider *LocalConnProvider) {
	for _, hook := range glbWrapperHooks {
		hook(provider)
	}
}

func (flag *TcpOptions) String() string {
	return fmt.Sprint(*flag)
}

func (flag *TcpOptions) Set(value string) error {
	optProtocol |= TCP
	for _, pair := range strings.Split(value, ",") {
		option := strings.Split(pair, ":")
		switch option[0] {
		case "read_timeout":
			data, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.readTimeout = data
		}
	}
	return nil
}

func (flag *KcpOptions) String() string {
	return fmt.Sprint(*flag)
}

func (flag *KcpOptions) Set(value string) error {
	optProtocol |= KCP
	// default vals
	flag.readTimeout = 60
	flag.sndWnd = 1024
	flag.rcvWnd = 1024
	flag.nodelay = 1
	flag.interval = 10
	flag.resend = 2
	flag.nc = 1
	flag.fecData = 0
	flag.fecParity = 0
	for _, pair := range strings.Split(value, ",") {
		option := strings.Split(pair, ":")
		switch option[0] {
		case "read_timeout":
			data, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.readTimeout = data
		case "snd_wnd":
			data, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.sndWnd = data
		case "rcv_wnd":
			data, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.rcvWnd = data
		case "nodelay":
			data, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.nodelay = data
		case "interval":
			data, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.interval = data
		case "resend":
			data, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.resend = data
		case "nc":
			data, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.nc = data
		case "fec_data":
			data, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.fecData = data
		case "fec_parity":
			parity, err := strconv.Atoi(option[1])
			if err != nil {
				return err
			}
			flag.fecParity = parity
		}
	}
	return nil
}

func main() {
	// deal with arguments
	var tcp TcpOptions
	var kcp KcpOptions
	var config string
	var listen string
	var reuseTimeout int
	var handshakeTimeout int
	var sentCacheSize int

	flag.Var(&tcp, "tcp", "tcp options, use default by setting empty literal")
	flag.Var(&kcp, "kcp", "kcp options, use default by setting empty literal")
	flag.StringVar(&config, "config", "./settings.conf", "backend servers config file")
	flag.StringVar(&listen, "listen", "0.0.0.0:1248", "local listen port(0.0.0.0:1248)")
	flag.IntVar(&logLevel, "log", 2, "larger value for detail log")
	flag.IntVar(&reuseTimeout, "reuseTimeout", 30, "reuse timeout")
	flag.IntVar(&handshakeTimeout, "handshakeTimeout", 30, "handshake stage timeout")
	flag.IntVar(&sentCacheSize, "sbuf", 65536, "sent cache size")
	flag.IntVar(&optUploadMinPacket, "uploadMinPacket", 0, "upload minimal packet")
	flag.IntVar(&optUploadMaxDelay, "uploadMaxDelay", 0, "upload maximal delay milliseconds")

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

	glbScpServer = NewSCPServer(&Options{
		reuseTimeout:     reuseTimeout,
		handshakeTimeout: handshakeTimeout,
		tcpOptions:       &tcp,
		kcpOptions:       &kcp,
	})

	var wg sync.WaitGroup

	if optProtocol == 0 || optProtocol&TCP != 0 {
		wg.Add(1)
		go func() {
			Log("tcp options: %v", tcp)
			glbScpServer.Start("tcp", listen)
			wg.Done()
		}()
	}
	if optProtocol&KCP != 0 {
		wg.Add(1)
		go func() {
			Log("kcp options: %v", kcp)
			glbScpServer.Start("kcp", listen)
			wg.Done()
		}()
	}

	wg.Wait()
}
