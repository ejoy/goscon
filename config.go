package main

import (
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/ejoy/goscon/scp"
	"github.com/ejoy/goscon/upstream"
	"github.com/spf13/viper"
	"github.com/xjdrew/glog"
	yaml "gopkg.in/yaml.v2"
)

var (
	zeroTime    time.Time
	configMu    sync.Mutex
	configCache map[string]interface{}
)

// init default configure
func init() {
	viper.SetDefault("manager", "127.0.0.1:6620") // manager: listen address, 为空表示不启用管理功能

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
	viper.SetDefault("kcp_option.opt_mtu", 1400)         // kcp mtu: 1400, 最大传输单元
	viper.SetDefault("kcp_option.opt_nodelay", 1)        // kcp opt_nodelay: 1, 是否启用 nodelay 模式
	viper.SetDefault("kcp_option.opt_interval", 10)      // kcp opt_interval: 10ms, kcp 调用 update 的时间间隔，单位是毫秒
	viper.SetDefault("kcp_option.opt_resend", 2)         // kcp opt_interval: 2,  快速重换模式，比如 2 表示：2 次 ACK 跨越直接重传
	viper.SetDefault("kcp_option.opt_nc", 1)             // kcp opt_nc: 1, 是否关闭拥塞控制，0-开启，1-关闭
	viper.SetDefault("kcp_option.opt_sndwnd", 2048)      // kcp opt_sndwnd: 2048 byte, kcp连接的发送窗口
	viper.SetDefault("kcp_option.opt_rcvwnd", 2048)      // kcp opt_rcvwnd: 2048 byte, kcp连接的接收窗口
	viper.SetDefault("kcp_option.opt_stream", true)      // kcp opt_stream: true, 是否启用kcp流模式; 流模式下，会合并udp包发送
	viper.SetDefault("kcp_option.opt_writedelay", false) // kcp opt_writedelay: false, 延迟到下次interval发送数据

	configCache = make(map[string]interface{})
}

func marshalConfigFile() (s string) {
	c := viper.AllSettings()
	b, err := yaml.Marshal(c)
	if err != nil {
		glog.Errorf("marshal failed: err=%s", err.Error())
		return
	}
	// print current config
	s = fmt.Sprintf(`####### goscon configuration #######
# config file %s
%s
####### end #######`, viper.ConfigFileUsed(), string(b))
	return
}

func reloadConfig() (err error) {
	glog.Info("load config")

	// try to load config from disk
	if viper.ConfigFileUsed() == "" {
		if glog.V(3) {
			glog.Error("no config file used")
		}
		return
	}

	if err = viper.ReadInConfig(); err != nil {
		glog.Errorf("read configuration failed: %s", err.Error())
		return
	}

	// print current config
	glog.Info(marshalConfigFile())

	// clear cache
	configMu.Lock()
	for k := range configCache {
		delete(configCache, k)
	}
	configMu.Unlock()

	// update upstream
	var hosts []upstream.Host
	if err = viper.UnmarshalKey("hosts", &hosts); err != nil {
		glog.Errorf("unmarshal hosts failed: %s", err.Error())
		return err
	}

	if err = upstream.UpdateHosts(hosts); err != nil {
		glog.Errorf("update hosts failed: %s", err.Error())
		return err
	}

	// update scp
	reuseBuffer := viper.GetInt("scp.reuse_buffer")
	if reuseBuffer > 0 {
		scp.RueseBufferSize = reuseBuffer
	}
	return
}

func configItemBool(name string) bool {
	configMu.Lock()
	defer configMu.Unlock()
	if v, ok := configCache[name]; ok {
		return v.(bool)
	}
	v := viper.GetBool(name)
	configCache[name] = v
	return v
}

func configItemInt(name string) int {
	configMu.Lock()
	defer configMu.Unlock()
	if v, ok := configCache[name]; ok {
		return v.(int)
	}
	v := viper.GetInt(name)
	configCache[name] = v
	return v
}

func configItemTime(name string) time.Duration {
	seconds := configItemInt(name)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
