// Package scp implements Stable Connection Protocol
package scp

import (
	"net"
)

type SCPServer interface {
	// allocate a id for connection
	AcquireID() int

	// release a id if handshake failed
	ReleaseID(id int)

	// query a conneciton by id
	QueryByID(id int) *Conn

	// close a conneciton by id
	CloseByID(id int) *Conn
}

type Config struct {
	// preferred target server
	// for client
	TargetServer string

	// reused conn
	// for client
	ConnForReused *Conn

	// SCPServer
	// for server
	ScpServer SCPServer
}

var defaultConfig = &Config{}

func (config *Config) clone() *Config {
	return &Config{
		ScpServer: config.ScpServer,
	}
}

func Server(conn net.Conn, config *Config) *Conn {
	if config.ScpServer == nil {
		panic("config.ScpServer == nil")
	}

	c := &Conn{
		conn:   conn,
		config: config.clone(),
	}

	return c
}

func Client(conn net.Conn, config *Config) *Conn {
	if config == nil {
		config = defaultConfig
	}

	c := &Conn{
		conn:   conn,
		config: config,
	}

	if config.ConnForReused != nil {
		if config.ConnForReused.id == 0 {
			panic("config.ConnForReused.id == 0")
		}
		c.initReuseConn(config.ConnForReused, config.ConnForReused.handshakes+1)
	}
	return c
}
