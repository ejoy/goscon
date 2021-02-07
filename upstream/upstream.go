package upstream

import (
	"errors"
	"math/rand"
	"net"
	"regexp"
	"sync/atomic"

	"github.com/ejoy/goscon/scp"
	"github.com/xjdrew/glog"
)

// ErrNoHost .
var ErrNoHost = errors.New("no host")

const defaultWeight = 100

// ResolveRule describes upstream resolver config
type ResolveRule struct {
	// prefix + name + suffix provides the domain name.
	Prefix string
	Suffix string

	Port string

	// The `targetServer` name must match the pattern.
	Pattern   string
	rePattern *regexp.Regexp
}

var errNoPort = errors.New("no port")

// Normalize .
func (r *ResolveRule) Normalize() error {
	if r.Pattern != "" {
		rePattern, err := regexp.Compile(r.Pattern)
		if err != nil {
			return err
		}
		r.rePattern = rePattern
	}
	if r.Port == "" {
		return errNoPort
	}
	return nil
}

// FullName returns hostport defined by rule.
func (r *ResolveRule) FullName(name string) string {
	return net.JoinHostPort(r.Prefix+name+r.Suffix, r.Port)
}

// Validate validates the `targetServer` name.
func (r *ResolveRule) Validate(name string) bool {
	if r.rePattern == nil {
		return true
	}
	return r.rePattern.MatchString(name)
}

// Option describes upstream option
type Option struct {
	Net    string
	Resolv *ResolveRule

	Hosts []*Host
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

// Upstreams 代表后端服务
type Upstreams struct {
	option atomic.Value // *Option

	allHosts    atomic.Value // *hostGroup
	byNameHosts atomic.Value // map[string]*hostGroup
}

// GetOption returns upstreams option.
func (u *Upstreams) GetOption() *Option {
	return u.option.Load().(*Option)
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

// SetOption .
func (u *Upstreams) SetOption(option *Option) error {
	// normalize resolv rule.
	if option.Resolv != nil {
		option.Resolv.Normalize()
	}

	// initialize the hosts.
	hosts := option.Hosts
	sz := len(hosts)
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
		allHosts.hosts = append(allHosts.hosts, h)
		allHosts.weight = allHosts.weight + h.Weight

		if h.Name != "" {
			hg := byNameHosts[h.Name]
			if hg == nil {
				hg = new(hostGroup)
				byNameHosts[h.Name] = hg
			}
			hg.hosts = append(hg.hosts, h)
			hg.weight = hg.weight + h.Weight
		}
	}

	u.option.Store(option)
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

func chooseByResolver(name string, rule *ResolveRule) *Host {
	hosts := make([]*Host, 0, 2)
	if rule.Validate(name) {
		hostport := rule.FullName(name)
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

// GetPreferredHost choose a host by name.
// If several hosts have same name then random choose by weight
func (u *Upstreams) GetPreferredHost(name string) *Host {
	mapHosts := u.byNameHosts.Load().(map[string]*hostGroup)
	h := chooseByLocalHosts(mapHosts[name])
	if h != nil {
		return h
	}
	option := u.option.Load().(*Option)
	if option.Resolv != nil {
		h = chooseByResolver(name, option.Resolv)
	}
	if h == nil {
		glog.Errorf("prefered name is malformed, name=%s", name)
	}
	return h
}

// GetRandomHost chooses a host randomly from all hosts.
func (u *Upstreams) GetRandomHost() *Host {
	mapHosts := u.allHosts.Load().(*hostGroup)
	return chooseByLocalHosts(mapHosts)
}

// GetHost prefers static hosts map, and will use resolver if config.
// When preferred is empty string, GetHost only searches static hosts map.
func (u *Upstreams) GetHost(preferred string) *Host {
	var h *Host
	if preferred != "" {
		h = u.GetPreferredHost(preferred)
	}
	if h == nil {
		h = u.GetRandomHost()
	}
	return h
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
