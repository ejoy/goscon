package main

import (
	"runtime"
	"time"

	"github.com/spf13/viper"
)

var (
	zeroTime time.Time
)

// init default configure
func init() {
	viper.SetDefault("tcp", "0.0.0.0:1248") // listen tcp: yes
	viper.SetDefault("kcp", "")             // listen kcp: no

	viper.SetDefault("scp.handshake_timeout", 30) // scp handshake_timeout: 30s
	viper.SetDefault("scp.reuse_time", 30)        // scp reuse_time: 30s
	viper.SetDefault("scp.reuse_buffer", 65536)   // scp reuse_buffer: 64kb

	viper.SetDefault("tcp_option.read_timeout", 0)        // tcp read_timeout: 0, never timeout
	viper.SetDefault("tcp_option.keepalive", true)        // tcp keepalive: true
	viper.SetDefault("tcp_option.keepalive_interval", 60) // tcp keepalive_interval: 60s

	viper.SetDefault("kcp_option.reuseport", runtime.NumCPU()) // kcp reuseport: count of cpu
	viper.SetDefault("kcp_option.read_timeout", 60)            // kcp read_timeout: 60s
	viper.SetDefault("kcp_option.fec_data_shards", 0)          // kcp fec, disable, refer to: https://www.backblaze.com/blog/reed-solomon/
	viper.SetDefault("kcp_option.fec_parity_shards", 0)
	viper.SetDefault("kcp_option.read_buffer", 4194304)  // kcp read_buffer: 4M
	viper.SetDefault("kcp_option.write_buffer", 4194304) // kcp write_buffer: 4M

	// kcp protocol configure, refer to: https://github.com/skywind3000/kcp/blob/master/README.en.md#protocol-configuration
	viper.SetDefault("kcp_option.opt_mtu", 1400)    // kcp mtu: 1400
	viper.SetDefault("kcp_option.opt_nodelay", 1)   // kcp opt_nodelay: 1
	viper.SetDefault("kcp_option.opt_interval", 10) // kcp opt_interval: 10ms
	viper.SetDefault("kcp_option.opt_resend", 2)    // kcp opt_interval: 2
	viper.SetDefault("kcp_option.opt_nc", 1)        // kcp opt_nc: 1
	viper.SetDefault("kcp_option.opt_sndwnd", 2048) // kcp opt_sndwnd: 2048 byte
	viper.SetDefault("kcp_option.opt_rcvwnd", 2048) // kcp opt_sndwnd: 2048 byte
}

func configItemTime(name string) time.Duration {
	seconds := viper.GetInt(name)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
