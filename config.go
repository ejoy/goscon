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

	viper.SetDefault("scp.handshake_timeout", 30) // scp handshake_timeout: 30s, scp握手超时时间
	viper.SetDefault("scp.reuse_time", 30)        // scp reuse_time: 30s, 客户端断开后，等待重用的时间
	viper.SetDefault("scp.reuse_buffer", 65536)   // scp reuse_buffer: 64kb, 等待重连期间，缓存发送给客户端的数据；合理值为reuse_time*流量速度

	viper.SetDefault("tcp_option.read_timeout", 0)        // tcp read_timeout: 0, never timeout
	viper.SetDefault("tcp_option.keepalive", true)        // tcp keepalive: true
	viper.SetDefault("tcp_option.keepalive_interval", 60) // tcp keepalive_interval: 60s

	viper.SetDefault("kcp_option.reuseport", runtime.NumCPU()) // kcp reuseport: count of cpu, 利用端口复用的特性，同时开启多个 Goroutine 监听端口，默认为cpu核数
	viper.SetDefault("kcp_option.read_timeout", 60)            // kcp read_timeout: 60s, 接收数据超时时间，超时会关闭客户端对应连接
	viper.SetDefault("kcp_option.fec_data_shards", 0)          // kcp fec, disable, refer to: https://www.backblaze.com/blog/reed-solomon/
	viper.SetDefault("kcp_option.fec_parity_shards", 0)
	viper.SetDefault("kcp_option.read_buffer", 4194304)  // kcp read_buffer: 4M, udp socket 的 RCV_BUF，单位为字节，默认为 4M
	viper.SetDefault("kcp_option.write_buffer", 4194304) // kcp write_buffer: 4M, udp socket 的 SND_BUF，单位为字节，默认为 4M

	// kcp protocol configure, refer to: https://github.com/skywind3000/kcp/blob/master/README.en.md#protocol-configuration
	viper.SetDefault("kcp_option.opt_mtu", 1400)    // kcp mtu: 1400, 最大传输单元
	viper.SetDefault("kcp_option.opt_nodelay", 1)   // kcp opt_nodelay: 1, 是否启用 nodelay 模式
	viper.SetDefault("kcp_option.opt_interval", 10) // kcp opt_interval: 10ms, kcp 调用 update 的时间间隔，单位是毫秒
	viper.SetDefault("kcp_option.opt_resend", 2)    // kcp opt_interval: 2,  快速重换模式，比如 2 表示：2 次 ACK 跨越直接重传
	viper.SetDefault("kcp_option.opt_nc", 1)        // kcp opt_nc: 1, 是否关闭拥塞控制，0-开启，1-关闭
	viper.SetDefault("kcp_option.opt_sndwnd", 2048) // kcp opt_sndwnd: 2048 byte, kcp连接的发送窗口
	viper.SetDefault("kcp_option.opt_rcvwnd", 2048) // kcp opt_sndwnd: 2048 byte, kcp连接的接收窗口
}

func configItemTime(name string) time.Duration {
	seconds := viper.GetInt(name)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
