// Package scp implements Stable Connection Protocol
package scp

import (
	"net"
	"time"
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
	// Flag
	// for client
	Flag int

	// preferred target server
	// for client
	TargetServer string

	// reused conn
	// for client
	ConnForReused *Conn

	// SCPServer
	// for server
	ScpServer SCPServer

	// ReuseBufferSize
	// stay the same during the connection life time.
	// for server and client
	ReuseBufferSize int

	// ReuseBufferPool
	// for optimize gc
	// for server and client
	ReuseBufferPool *LoopBufferPool

	// HandshakeTimeout
	HandshakeTimeout time.Duration
}

var defaultConfig = &Config{ReuseBufferSize: ReuseBufferSize}

func (config *Config) clone() *Config {
	return &Config{
		ScpServer:        config.ScpServer,
		ReuseBufferSize:  config.ReuseBufferSize,
		ReuseBufferPool:  config.ReuseBufferPool,
		HandshakeTimeout: config.HandshakeTimeout,
	}
}

func (config *Config) setDefault() {
	if config.ReuseBufferSize == 0 {
		config.ReuseBufferSize = ReuseBufferSize
	}
}

// Server wraps conn as scp.Conn
func Server(conn net.Conn, config *Config) *Conn {
	if config.ScpServer == nil {
		panic("config.ScpServer == nil")
	}

	c := &Conn{
		conn:   conn,
		config: config.clone(),
	}
	c.config.setDefault()

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
	c.config.setDefault()

	if config.ConnForReused != nil {
		if !config.ConnForReused.spawn(c) {
			return nil, ErrNotAcceptable
		}
		c.handshakes = config.ConnForReused.handshakes + 1
	}

	return c, nil
}
