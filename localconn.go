package main

import (
	"net"
	"sync"

	"github.com/ejoy/goscon/scp"
	"github.com/xjdrew/glog"
)

// LocalSCPConn .
type LocalSCPConn struct {
	*SCPConn
}

// ReuseConn reused conn
func reuseConn(connForReused *scp.Conn) (conn net.Conn, err error) {
	addr, _ := net.ResolveTCPAddr("tcp", connForReused.RemoteAddr().String())
	tcpConn, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		glog.Errorf("connect to <%s> failed: %s when reuse conn", addr.String(), err.Error())
		return
	}

	scon, err := scp.Client(tcpConn, &scp.Config{
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

	conn = scon
	return
}

func (c *LocalSCPConn) reuseConn() {
	conn, err := reuseConn(c.Conn)
	if err != nil {
		return
	}
	scon := conn.(*scp.Conn)
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
func NewLocalSCPConn(scon *scp.Conn) *LocalSCPConn {
	scpConn := &SCPConn{Conn: scon}
	scpConn.connCond = sync.NewCond(&scpConn.connMutex)
	scpConn.reuseTimeout = configItemTime("scp.reuse_time")
	localSCPConn := &LocalSCPConn{SCPConn: scpConn}
	return localSCPConn
}
