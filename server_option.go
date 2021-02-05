package main

import (
	"time"

	"github.com/ejoy/goscon/upstream"
)

// SCPOption configs scp option.
type SCPOption struct {
	HandshakeTimeout time.Duration `mapstructure:"handshake_timeout"`
	ReuseTime        time.Duration `mapstructure:"reuse_time"`
	ReuseBuffer      int           `mapstructure:"reuse_buffer"`
}

// TCPOption configs tcp option.
type TCPOption struct {
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	Keepalive         bool          `mapstructure:"keepalive"`
	KeepaliveInterval time.Duration `mapstructure:"keepalive_interval"`
}

// KCPOption configs kcp option.
type KCPOption struct {
	ReusePort       int           `mapstructure:"reuseport"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	FecDataShards   int           `mapstructure:"fec_data_shards"`
	FecParityShards int           `mapstructure:"fec_parity_shards"`
	ReadBuffer      int           `mapstructure:"read_buffer"`
	WriteBuffer     int           `mapstructure:"write_buffer"`
	OptMTU          int           `mapstructure:"opt_mtu"`
	OptNodelay      int           `mapstructure:"opt_nodelay"`
	OptInterval     int           `mapstructure:"opt_interval"`
	OptResend       int           `mapstructure:"opt_resend"`
	OptNC           int           `mapstructure:"opt_nc"`
	OptSndwnd       int           `mapstructure:"opt_sndwnd"`
	OptRcvwnd       int           `mapstructure:"opt_rcvwnd"`
	OptStream       bool          `mapstructure:"opt_stream"`
	OptWriteDelay   bool          `mapstructure:"opt_writedelay"`
}

// Option configs server option.
type Option struct {
	TCP            string           `mapstructure:"tcp"`
	KCP            string           `mapstructure:"kcp"`
	TCPOption      *TCPOption       `mapstructure:"tcp_option"`
	KCPOption      *KCPOption       `mapstructure:"kcp_option"`
	SCPOption      *SCPOption       `mapstructure:"scp_option"`
	UpstreamOption *upstream.Option `mapstructure:"upstream_option"`
}
