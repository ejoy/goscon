// Package scp implements Stable Connection Protocol
package scp

import (
	"io"
	"net"
)

func Server(conn net.Conn) *Conn {
	return nil
}

func Client(conn net.Conn, oldConn *Conn) *Conn {
	c := &Conn{
		conn:     conn,
		isClient: true,
	}

	if oldConn != nil {
		if oldConn.id == 0 {
			panic("oldConn.id == 0")
		}

		c.id = oldConn.id
		c.handshakes = oldConn.handshakes + 1
		c.secret = oldConn.secret

		c.sentCache = deepCopyLoopBuffer(oldConn.sentCache)
		c.in = deepCopyCipherConnReader(oldConn.in)
		c.out = deepCopyCipherConnWriter(oldConn.out)

		c.in.SetReader(conn)
		c.out.SetWriter(io.MultiWriter(c.sentCache, conn))
	}
	return c
}
