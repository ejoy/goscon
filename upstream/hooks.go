package upstream

import (
	"net"

	"github.com/ejoy/goscon/scp"
)

// Hook defines hook interface for upstream
type Hook interface {
	IfEnable(local net.Conn, remote *scp.Conn) bool
	AfterConnected(local net.Conn, remote *scp.Conn) (err error)
}

// installed hook
var upstreamHook Hook

func setHook(hook Hook) {
	if upstreamHook != nil {
		panic("setHook again")
	}
	upstreamHook = hook
}

// IfEnable check upstream hook whether enabled for the connection pair
func IfEnable(local net.Conn, remote *scp.Conn) bool {
	if upstreamHook == nil {
		return true
	}
	return upstreamHook.IfEnable(local, remote)
}

// OnAfterConnected call when upstream connection is connected
func OnAfterConnected(local net.Conn, remote *scp.Conn) (err error) {
	if upstreamHook == nil {
		return
	}
	err = upstreamHook.AfterConnected(local, remote)
	return
}
