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

	// Flag
	// for server
	Flag int
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

// Client wraps conn as scp.Conn
func Client(conn net.Conn, config *Config) (*Conn, error) {
	if config == nil {
		config = defaultConfig
	}

	c := &Conn{
		conn:   conn,
		config: config,
	}

	if config.ConnForReused != nil {
		if !config.ConnForReused.spawn(c) {
			return nil, ErrNotAcceptable
		}
		c.handshakes = config.ConnForReused.handshakes + 1
	}
	return c, nil
}
