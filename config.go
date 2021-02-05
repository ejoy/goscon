package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/ejoy/goscon/upstream"
	"github.com/spf13/viper"
	"github.com/xjdrew/glog"
	yaml "gopkg.in/yaml.v2"
)

// ErrInvalidConfig .
var ErrInvalidConfig = errors.New("Invalid Config")

// ErrTCPOrKCP .
var ErrTCPOrKCP = errors.New("Need tcp or kcp listen")

// ErrNoHostOrResolv .
var ErrNoHostOrResolv = errors.New("No hosts or resolv rule configed")

// ViperConfigSchema .
type ViperConfigSchema struct {
	Manager        string             `mapstructure:"manager"`
	SCPOption      *SCPOption         `mapstructure:"scp_option"`
	TCPOption      *TCPOption         `mapstructure:"tcp_option"`
	KCPOption      *KCPOption         `mapstructure:"kcp_option"`
	UpstreamOption *upstream.Option   `mapstructure:"upstream_option"`
	Server         map[string]*Option `mapstructure:"server"`
}

// ViperConfig .
type ViperConfig struct {
	mu sync.Mutex

	current    *viper.Viper
	configFile string
}

var (
	viperConfig *ViperConfig
)

func init() {
	viperConfig = &ViperConfig{}
}

// setConfigFile .
func (v *ViperConfig) setConfigFile(file string) {
	v.configFile = file
}

// set default configure
func (v *ViperConfig) setDefault(viper *viper.Viper) {
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

	viper.SetDefault("upstream_option.net", "tcp") // upstream net: tcp,  默认使用 tcp 连接后端服务器，可以指定使用 scp 协议保证连接自动重连。
}

func (v *ViperConfig) marshalConfigFile(viper *viper.Viper) (s string) {
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

func checkUpstreamOption(viper *viper.Viper, key string) error {
	var upstreamOption upstream.Option
	viper.UnmarshalKey(key, &upstreamOption)
	if upstreamOption.Resolv != nil {
		if err := upstreamOption.Resolv.Normalize(); err != nil {
			glog.Errorf("invalid pattern for validates the domain name: %s", err.Error())
			return err
		}
	}
	return nil
}

func checkDefaultOption(viper *viper.Viper) error {
	if err := checkUpstreamOption(viper, "upstream_option"); err != nil {
		return err
	}
	return nil
}

func checkServerOption(viper *viper.Viper, serverType string) error {
	if !(viper.IsSet(serverType+".tcp") || viper.IsSet(serverType+".kcp")) {
		return ErrTCPOrKCP
	}
	if err := checkUpstreamOption(viper, serverType+".upstream_option"); err != nil {
		return err
	}
	if !(viper.IsSet(serverType+".upstream_option.resolv") || viper.IsSet(serverType+".upstream_option.hosts")) {
		return ErrNoHostOrResolv
	}
	return nil
}

// reloadConfig .
func (v *ViperConfig) reloadConfig() (err error) {
	glog.Info("reload config")

	// try to load config from disk
	if v.configFile == "" {
		if glog.V(3) {
			glog.Error("no config file used")
		}
		return ErrInvalidConfig
	}

	newViper := viper.New()
	newViper.SetConfigFile(v.configFile)

	if err = newViper.ReadInConfig(); err != nil {
		glog.Errorf("read configuration failed: %s", err.Error())
		return ErrInvalidConfig
	}

	// print the new config
	if configString := v.marshalConfigFile(newViper); configString != "" {
		glog.Info(configString)
	} else {
		return ErrInvalidConfig
	}

	var config ViperConfigSchema
	if err := newViper.Unmarshal(&config); err != nil {
		glog.Errorf("Config file is invalid: %s", err.Error())
		return err
	}

	if err := checkDefaultOption(newViper); err != nil {
		glog.Errorf("check default option failed: %s", err.Error())
		return err
	}

	server := newViper.GetStringMap("server")
	for typ := range server {
		if err := checkServerOption(newViper, "server."+typ); err != nil {
			glog.Errorf("check server:%s option failed: %s", typ, err.Error())
			return err
		}
	}

	v.setDefault(newViper)

	v.mu.Lock()
	v.current = newViper
	v.mu.Unlock()

	return
}

// getServers .
func (v *ViperConfig) getServers() []string {
	var tempViper *viper.Viper
	v.mu.Lock()
	tempViper = v.current
	v.mu.Unlock()
	servers := tempViper.GetStringMap("server")
	serverList := make([]string, 0, len(servers))
	for s := range servers {
		serverList = append(serverList, s)
	}
	return serverList
}

func setDefaultField(viper *viper.Viper, m map[string]interface{}, root string, fields ...string) {
	dotField := strings.Join(fields, ".")
	if !viper.IsSet(root + "." + dotField) {
		leaf := m
		lastIndex := len(fields) - 1
		for i := 0; i != lastIndex; i++ {
			leaf = leaf[fields[i]].(map[string]interface{})
		}
		leaf[fields[lastIndex]] = viper.Get(dotField)
	}
}

// getServerOption .
func (v *ViperConfig) getServerOption(typ string) *Option {
	var tempViper *viper.Viper
	v.mu.Lock()
	tempViper = v.current
	v.mu.Unlock()

	serverType := "server." + typ
	if !tempViper.IsSet(serverType) {
		return nil
	}
	optionMap := tempViper.GetStringMap(serverType)
	// tcp option
	if tempViper.IsSet(serverType + ".tcp") {
		setDefaultField(tempViper, optionMap, serverType, "tcp_option")
		setDefaultField(tempViper, optionMap, serverType, "tcp_option", "read_timeout")
		setDefaultField(tempViper, optionMap, serverType, "tcp_option", "keepalive")
		setDefaultField(tempViper, optionMap, serverType, "tcp_option", "keepalive_interval")
	}
	// kcp option
	if tempViper.IsSet(serverType + ".kcp") {
		setDefaultField(tempViper, optionMap, serverType, "kcp_option")
		for field := range tempViper.GetStringMap("kcp_option") {
			setDefaultField(tempViper, optionMap, serverType, "kcp_option", field)
		}
	}
	// scp option
	setDefaultField(tempViper, optionMap, serverType, "scp_option")
	setDefaultField(tempViper, optionMap, serverType, "scp_option", "handshake_timeout")
	setDefaultField(tempViper, optionMap, serverType, "scp_option", "reuse_time")
	setDefaultField(tempViper, optionMap, serverType, "scp_option", "reuse_buffer")
	// upstream option
	setDefaultField(tempViper, optionMap, serverType, "upstream_option")
	setDefaultField(tempViper, optionMap, serverType, "upstream_option", "net")

	var option Option
	tempViper.UnmarshalKey(serverType, &option)
	return &option
}

func (v *ViperConfig) get(key string) interface{} {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.current.Get(key)
}

// GetConfigManager .
func GetConfigManager() string {
	return viperConfig.get("manager").(string)
}

// SetConfigFile .
func SetConfigFile(file string) {
	viperConfig.setConfigFile(file)
}

// MarshalConfigFile .
func MarshalConfigFile() string {
	return viperConfig.marshalConfigFile(viperConfig.current)
}

// ReloadConfig .
func ReloadConfig() error {
	return viperConfig.reloadConfig()
}

// GetConfigServers .
func GetConfigServers() []string {
	return viperConfig.getServers()
}

// GetConfigServerOption .
func GetConfigServerOption(typ string) *Option {
	return viperConfig.getServerOption(typ)
}
