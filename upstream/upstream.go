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

// Option decribes upstream option
type Option struct {
	Net string
}

// Host indicates a backend server
type Host struct {
	Name   string
	Addr   string
	Weight int
	Net    string

	addrs []*net.TCPAddr
}

type hostGroup struct {
	hosts  []*Host
	weight int
}

// upstreams 代表后端服务
type upstreams struct {
	option Option

	allHosts    atomic.Value // *hostGroup
	byNameHosts atomic.Value // map[string]*hostGroup
}

// SetOption .
func (u *upstreams) SetOption(option Option) error {
	u.option = option
	return nil
}

func (u *upstreams) chooseByWeight(group *hostGroup) *Host {
	if group == nil || len(group.hosts) == 0 {
		return nil
	}

	v := rand.Intn(group.weight)
	for _, host := range group.hosts {
		if host.Weight >= v {
			return host
		}
		v -= host.Weight
	}
	return nil
}

// reference to the host:port format of `net.Dial`.
func lookupTCPAddrs(hostport string) ([]*net.TCPAddr, error) {
	host, service, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil, err
	}
	tcpAddrs := make([]*net.TCPAddr, len(addrs))
	for i, addr := range addrs {
		addr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(addr, service))
		if err != nil { // only error when lookup port failed
			return nil, err
		}
		tcpAddrs[i] = addr
	}
	return tcpAddrs, nil
}

// UpdateHosts .
func (u *upstreams) UpdateHosts(hosts []Host) error {
	sz := len(hosts)
	if sz == 0 {
		return ErrNoHost
	}
	allHosts := new(hostGroup)
	allHosts.hosts = make([]*Host, 0, sz)
	allHosts.weight = 0

	byNameHosts := make(map[string]*hostGroup)
	for _, host := range hosts {
		h := host
		addrs, err := lookupTCPAddrs(h.Addr)
		if err != nil {
			return err
		}
		h.addrs = addrs
		if h.Weight <= 0 {
			// set default weight
			h.Weight = defaultWeight
		}
		allHosts.hosts = append(allHosts.hosts, &h)
		allHosts.weight = allHosts.weight + h.Weight

		if h.Name != "" {
			hg := byNameHosts[h.Name]
			if hg == nil {
				hg = new(hostGroup)
				byNameHosts[h.Name] = hg
			}
			hg.hosts = append(hg.hosts, &h)
			hg.weight = hg.weight + h.Weight
		}
	}

	u.allHosts.Store(allHosts)
	u.byNameHosts.Store(byNameHosts)
	return nil
}

// GetHostByWeight random choose a host by weight
func (u *upstreams) GetHostByWeight() *Host {
	hosts := u.allHosts.Load().(*hostGroup)
	return u.chooseByWeight(hosts)
}

// GetHostByName choose a host by name, if several hosts have same
// name then random choose by weight
func (u *upstreams) GetHostByName(name string) *Host {
	mapHosts := u.byNameHosts.Load().(map[string]*hostGroup)
	return u.chooseByWeight(mapHosts[name])
}

// GetHost .
func (u *upstreams) GetHost(preferred string) *Host {
	if preferred != "" {
		return u.GetHostByName(preferred)
	}
	return u.GetHostByWeight()
}

func upgradeNetConn(network string, localConn net.Conn, remoteConn *scp.Conn) (conn net.Conn, err error) {
	if network == "scp" {
		scon, _ := scp.Client(localConn, &scp.Config{TargetServer: remoteConn.TargetServer()})

		err = scon.Handshake()
		if err != nil {
			glog.Errorf("scp handshake failed: client=%s, err=%s", scon.RemoteAddr().String(), err.Error())
			scon.Close()
			return
		}
		conn = scon
	} else {
		conn = localConn
	}
	return
}

// NewConn creates a new connection to target server, pair with remoteConn
func (u *upstreams) NewConn(remoteConn *scp.Conn) (conn net.Conn, err error) {
	tserver := remoteConn.TargetServer()
	host := u.GetHost(tserver) // TODO: handle name resolve
	if host == nil {
		err = ErrNoHost
		glog.Errorf("get host <%s> failed: %s", tserver, err.Error())
		return
	}

	addr := host.addrs[rand.Intn(len(host.addrs))]
	tcpConn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		glog.Errorf("connect to <%s> failed: %s", host.Addr, err.Error())
		return
	}

	conn, err = upgradeNetConn(u.option.Net, tcpConn, remoteConn)
	if err != nil {
		return
	}

	err = OnAfterConnected(conn, remoteConn)
	return
}

var defaultUpstreams upstreams

// SetOption sets option
func SetOption(option Option) error {
	return defaultUpstreams.SetOption(option)
}

// UpdateHosts refresh backend hosts list
func UpdateHosts(hosts []Host) error {
	return defaultUpstreams.UpdateHosts(hosts)
}

// NewConn create a new connection, pair with remoteConn
func NewConn(remoteConn *scp.Conn) (conn net.Conn, err error) {
	return defaultUpstreams.NewConn(remoteConn)
}
