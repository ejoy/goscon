package main

import (
	"net"

	"github.com/ejoy/goscon/scp"
	"github.com/xjdrew/glog"
)

// LocalSCPConn .
type LocalSCPConn struct {
	*SCPConn
}

// ReuseConn reused conn
func reuseConn(connForReused *scp.Conn) (scon *scp.Conn, err error) {
	remoteAddr := connForReused.RemoteAddr()
	tcpConn, err := net.Dial(remoteAddr.Network(), remoteAddr.String())
	if err != nil {
		glog.Errorf("connect to <%s> failed: %s when reuse conn", remoteAddr.String(), err.Error())
		return
	}

	scon, err = scp.Client(tcpConn, &scp.Config{
		ConnForReused: connForReused,
		TargetServer:  connForReused.TargetServer(),
	})
	if err != nil {
		glog.Errorf("scp reuse conn failed: %s", err.Error())
		return
	}

	err = scon.Handshake()
	if err != nil {
		glog.Errorf("scp reuse handshake failed: client=%s, err=%s", scon.RemoteAddr().String(), err.Error())
		scon.Close()
		return
	}

	return
}

func (c *LocalSCPConn) reuseConn() {
	scon, err := reuseConn(c.Conn)
	if err != nil {
		return
	}
	if !c.ReplaceConn(scon) {
		scon.Close()
	}
}

// startWait start reuse timer and reuse conn
func (c *LocalSCPConn) startWait() {
	c.SCPConn.startWait()
	go c.reuseConn() // reuse the upstream scp.conn
}

// NewLocalSCPConn .
func NewLocalSCPConn(scon *scp.Conn, option *SCPOption) *LocalSCPConn {
	scpConn := NewSCPConn(scon, option)
	localSCPConn := &LocalSCPConn{SCPConn: scpConn}
	return localSCPConn
}
