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

// Host indicates a backend server
type Host struct {
	Addr   string
	Weight int
	Name   string

	addrs []*net.TCPAddr
}

type hostGroup struct {
	hosts  []*Host
	weight int
}

// upstreams 代表后端服务
type upstreams struct {
	allHosts    atomic.Value // *hostGroup
	byNameHosts atomic.Value // map[string]*hostGroup
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

func lookupTCPAddrs(hostport string) ([]*net.TCPAddr, error) {
	host, service, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil, err
	}
	port, err := net.LookupPort("tcp", service)
	if err != nil {
		return nil, err
	}
	tcpAddrs := make([]*net.TCPAddr, len(addrs))
	for i, addr := range addrs {
		tcpAddrs[i] = &net.TCPAddr{
			IP:   net.ParseIP(addr),
			Port: port,
			Zone: "",
		}
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

// NewConn create a new connection, pair with remoteConn
func (u *upstreams) NewConn(remoteConn *scp.Conn) (conn net.Conn, err error) {
	tserver := remoteConn.TargetServer()
	host := u.GetHost(tserver)
	if host == nil {
		err = ErrNoHost
		glog.Errorf("get host <%s> failed: %s", tserver, err.Error())
		return
	}

	addr := host.addrs[rand.Intn(len(host.addrs))]
	conn, err = net.DialTCP("tcp", nil, addr)
	if err != nil {
		glog.Errorf("connect to <%s> failed: %s", host.Addr, err.Error())
		return
	}
	err = OnAfterConnected(conn, remoteConn)
	return
}

var defaultUpstreams upstreams

// NewConn create a new connection, pair with remoteConn
func NewConn(remoteConn *scp.Conn) (conn net.Conn, err error) {
	return defaultUpstreams.NewConn(remoteConn)
}

// UpdateHosts refresh backend hosts list
func UpdateHosts(hosts []Host) error {
	return defaultUpstreams.UpdateHosts(hosts)
}
