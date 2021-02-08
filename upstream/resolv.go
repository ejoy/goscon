package upstream

import (
	"errors"
	"net"
	"regexp"
	"sync/atomic"
)

// Name resolve search the hosts firstly,
// then search by the resolve rule if `resolv` is true.

var errInvalidHost = errors.New("invalid hosts config")
var errNoPort = errors.New("no port")

var (
	configHosts   atomic.Value // []*Hosts
	configHostMap atomic.Value // map[string]*resolveResult
	configResolv  atomic.Value // ResolveRule
)

type resolveResult struct {
	hosts []*Host
}

// Host indicates a backend server
type Host struct {
	Name string
	Addr string // hostport address, and host must be ip.

	addr *net.TCPAddr
}

// Normalize .
func (h *Host) Normalize() error {
	host, service, err := net.SplitHostPort(h.Addr)
	if err != nil {
		return err
	}
	if net.ParseIP(host) == nil {
		return errInvalidHost
	}
	if addr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(host, service)); err != nil {
		return err
	} else {
		h.addr = addr
	}
	return nil
}

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

// 1. Search the local host map firstly.
// 2. Search based on the resolv rule.
func resolvName(name string, resolv bool) []*Host {
	hostMap := configHostMap.Load().(map[string]*resolveResult)
	if rr, exist := hostMap[name]; exist {
		return rr.hosts
	}
	hosts := make([]*Host, 0, 2)
	rule := configResolv.Load().(*ResolveRule)
	if rule.Validate(name) {
		hostport := rule.FullName(name)
		if addr, err := net.ResolveTCPAddr("tcp", hostport); err == nil {
			hosts = append(hosts, &Host{Name: name, Addr: hostport, addr: addr})
		}
	}
	if len(hosts) == 0 {
		return nil
	}
	return hosts
}

// ReloadHosts reloads the global hosts config.
func ReloadHosts(hosts []*Host) {
	hostMap := make(map[string]*resolveResult)
	for _, h := range hosts {
		if _, e := hostMap[h.Name]; !e {
			hostMap[h.Name] = new(resolveResult)
		}
		rr := hostMap[h.Name]
		rr.hosts = append(rr.hosts, h)
	}

	configHosts.Store(hosts)
	configHostMap.Store(hostMap)
}

// ReloadResolv reloads the global resolv config.
func ReloadResolv(resolv *ResolveRule) {
	configResolv.Store(resolv)
}
