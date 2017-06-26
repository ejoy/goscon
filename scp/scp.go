// Package scp implements Stable Connection Protocol
package scp

import (
	"net"
)

type SCPServer interface {
	// allocate a id for connection
	AcquireID() int

	// query a conneciton by id
	QueryByID(id int) *Conn

	// close a conneciton by id
	CloseByID(id int) *Conn
}

func Server(conn net.Conn, ss SCPServer) *Conn {
	c := &Conn{
		conn:      conn,
		scpServer: ss,
	}

	return c
}

func Client(conn net.Conn, oldConn *Conn) *Conn {
	c := &Conn{
		conn: conn,
	}

	if oldConn != nil {
		if oldConn.id == 0 {
			panic("oldConn.id == 0")
		}
		c.initReuseConn(oldConn, oldConn.handshakes+1)
	}
	return c
}
