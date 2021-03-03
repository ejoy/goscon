package upstream

import (
	"errors"
	"math/rand"
	"net"
	"sync/atomic"

	"github.com/ejoy/goscon/scp"
	"github.com/xjdrew/glog"
)

// ErrNoHost .
var ErrNoHost = errors.New("no host")

const defaultWeight = 100

// Server .
type Server struct {
	Name   string
	Weight int
}

// Option describes upstream option.
// `Net` indicates the protocol to use for upstream connection.
// `Servers` is where the scon will be routed to when `TargetServer` is not assigned.
// `Resolv` is true indicates that using the `Resolv` rule for name resolv.
type Option struct {
	Net    string
	Resolv bool

	Servers []*Server
	Weight  int
}

// Upstreams 代表后端服务
type Upstreams struct {
	option atomic.Value // *Option
}

// SetOption .
func (u *Upstreams) SetOption(option *Option) error {
	for _, h := range option.Servers {
		if h.Weight <= 0 {
			h.Weight = defaultWeight
		}
		option.Weight += h.Weight
	}
	u.option.Store(option)
	return nil
}

func chooseByWeight(servers []*Server, weight int) string {
	if len(servers) == 0 {
		return ""
	}
	if weight == 0 {
		return ""
	}

	v := rand.Intn(weight)
	for _, l := range servers {
		if l.Weight >= v {
			return l.Name
		}
		v -= l.Weight
	}
	return ""
}

// GetPreferredHost choose hosts by name.
func (u *Upstreams) GetPreferredHost(name string) []*Host {
	option := u.option.Load().(*Option)
	return resolvName(name, option.Resolv)
}

// GetRandomHost chooses hosts randomly from all servers.
func (u *Upstreams) GetRandomHost() []*Host {
	option := u.option.Load().(*Option)
	name := chooseByWeight(option.Servers, option.Weight)
	return resolvName(name, option.Resolv)
}

// GetHost prefers static hosts map, and will use resolver if config.
// When preferred is empty string, GetHost only searches static hosts map.
func (u *Upstreams) GetHost(preferred string) []*Host {
	var hosts []*Host
	if preferred != "" {
		hosts = u.GetPreferredHost(preferred)
	} else {
		hosts = u.GetRandomHost()
	}
	return hosts
}

func upgradeConn(network string, localConn net.Conn, remoteConn *scp.Conn) (conn net.Conn, err error) {
	if network == "scp" {
		scon, _ := scp.Client(localConn, &scp.Config{
			Flag: scp.SCPFlagForbidForwardIP,

			TargetServer: remoteConn.TargetServer(),
			// Keeps ReuseBuffer consistent with the remote conn (downstream).
			ReuseBufferSize: remoteConn.ReuseBufferSize(),
			ReuseBufferPool: remoteConn.ReuseBufferPool(),
		})

		err = scon.Handshake()
		if err != nil {
			glog.Errorf("scp handshake failed: client=%s, err=%s", scon.RemoteAddr().String(), err.Error())
			return
		}
		conn = scon
	} else {
		conn = localConn
	}
	return
}

// NewConn creates a new connection to target server, pair with remoteConn
func (u *Upstreams) NewConn(remoteConn *scp.Conn) (conn net.Conn, err error) {
	tserver := remoteConn.TargetServer()
	hosts := u.GetHost(tserver)
	if len(hosts) == 0 {
		err = ErrNoHost
		glog.Errorf("get host <%s> failed: %s", tserver, err.Error())
		return
	}

	rand.Shuffle(len(hosts), func(i, j int) { hosts[i], hosts[j] = hosts[j], hosts[i] })
	var tcpConn *net.TCPConn
	for _, host := range hosts {
		tcpConn, err = net.DialTCP("tcp", nil, host.addr)
		if err == nil {
			break
		}
	}
	if err != nil {
		glog.Errorf("connect to <%s> failed: %s", tserver, err.Error())
		return
	}

	option := u.option.Load().(*Option)
	conn, err = upgradeConn(option.Net, tcpConn, remoteConn)
	if err != nil {
		conn.Close()
		return
	}

	err = OnAfterConnected(conn, remoteConn)
	return
}

// New returns upstreams.
func New(option *Option) (*Upstreams, error) {
	u := &Upstreams{}
	if err := u.SetOption(option); err != nil {
		return nil, err
	}
	return u, nil
}
