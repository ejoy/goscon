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

// ResolveRule describes upstream resolver config
type ResolveRule struct {
	Prefix string
	Suffix string
	Port   string
}

// Normalize .
func (r *ResolveRule) Normalize(defaultPort string) bool {
	if r.Prefix == "" && r.Suffix == "" {
		return false
	}
	if r.Port == "" {
		r.Port = defaultPort
	}
	return true
}

// UniqueID identifies the rule.
func (r *ResolveRule) UniqueID() string {
	return r.FullName("")
}

// FullName returns hostport defined by rule.
func (r *ResolveRule) FullName(name string) string {
	return net.JoinHostPort(r.Prefix+name+r.Suffix, r.Port)
}

// Option describes upstream option
type Option struct {
	Net          string
	ResolveRules []*ResolveRule
}

// Host indicates a backend server
type Host struct {
	Name   string
	Addr   string
	Weight int

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

func chooseByLocalHosts(group *hostGroup) *Host {
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

func chooseByResolver(name string, rules []*ResolveRule) *Host {
	hosts := make([]*Host, 0, 2)
	for _, r := range rules {
		hostport := r.FullName(name)
		addrs, err := lookupTCPAddrs(hostport)
		if err == nil {
			h := Host{
				Name:  name,
				Addr:  hostport,
				addrs: addrs,
			}
			hosts = append(hosts, &h)
		}
	}
	lenhosts := len(hosts)
	if lenhosts == 0 {
		return nil
	}
	return hosts[rand.Intn(lenhosts)]
}

// GetPreferedHost choose a host by name, if several hosts have same
// name then random choose by weight
func (u *upstreams) GetPreferredHost(name string) *Host {
	mapHosts := u.byNameHosts.Load().(map[string]*hostGroup)
	h := chooseByLocalHosts(mapHosts[name])
	if h == nil && len(u.option.ResolveRules) > 0 {
		h = chooseByResolver(name, u.option.ResolveRules)
	}
	return h
}

// GetRandomHost chooses a host randomly from all hosts.
func (u *upstreams) GetRandomHost() *Host {
	mapHosts := u.allHosts.Load().(*hostGroup)
	return chooseByLocalHosts(mapHosts)
}

// GetHost prefers static hosts map, and will use resolver if config.
// When preferred is empty string, GetHost only searches static hosts map.
func (u *upstreams) GetHost(preferred string) *Host {
	if preferred != "" {
		return u.GetPreferredHost(preferred)
	}
	return u.GetRandomHost()
}

func upgradeConn(network string, localConn net.Conn, remoteConn *scp.Conn) (conn net.Conn, err error) {
	if network == "scp" {
		scon, _ := scp.Client(localConn, &scp.Config{TargetServer: remoteConn.TargetServer()})

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

	conn, err = upgradeConn(u.option.Net, tcpConn, remoteConn)
	if err != nil {
		conn.Close()
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
