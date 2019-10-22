//
//   date  : 2014-05-23 17:35
//   author: xjdrew
//

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/ejoy/goscon/scp"
	"github.com/ejoy/goscon/upstream"
	"github.com/golang/glog"
	"github.com/spf13/viper"
	yaml "gopkg.in/yaml.v2"
)

// log rule:
// verbose:
// 0, default, 确定数量的日志
// 1, connection related, 跟连接相关的日志， 一般每条连接在生命周期内产生x条
// 2, packet releated, 跟包相关的日志，一般每个packet都会产生数条日志

// 1.0.0: 2019-10-18, 重写goscon的配置方式
var _Version = "1.0.0"

const sigReload = syscall.Signal(34)
const sigStatus = syscall.Signal(35)

func status() {
	Log("status:\n\t"+
		"procs:%d/%d\n\t"+
		"goroutines:%d\n\t"+
		"actives:%d",
		runtime.GOMAXPROCS(0), runtime.NumCPU(),
		runtime.NumGoroutine(),
		defaultServer.NumOfConnPairs())
}

func handleSignal() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, sigReload, sigStatus, syscall.SIGTERM)

	for sig := range c {
		glog.Infof("receive signal: %v", sig)
		switch sig {
		case sigReload:
			reloadConfig()
		case sigStatus:
			status()
		case syscall.SIGTERM:
			glog.Infof("ignore signal: %v", sig)
		}
	}
}

var (
	optProtocol        = 0
	optUploadMinPacket int
	optUploadMaxDelay  int
)

func testConfigFile(filename string) error {
	viper.SetConfigFile(filename)
	return viper.ReadInConfig()
}

func marshalConfigFile() (s string) {
	c := viper.AllSettings()
	b, err := yaml.Marshal(c)
	if err != nil {
		glog.Errorf("marshal failed: err=%s", err.Error())
		return
	}
	// print current config
	s = fmt.Sprintf(`config %s =====>>>
%s
<<<=====`, viper.ConfigFileUsed(), string(b))
	return
}

func reloadConfig() (err error) {
	glog.Info("load config")

	// try to load config from disk
	if viper.ConfigFileUsed() == "" {
		if glog.V(3) {
			glog.Error("no config file used")
		}
	} else {
		if err = viper.ReadInConfig(); err != nil {
			glog.Errorf("read configuration failed: %s", err.Error())
			return
		}
	}

	// print current config
	glog.Info(marshalConfigFile())

	// update upstream
	var hosts []upstream.Host
	if err = viper.UnmarshalKey("hosts", &hosts); err != nil {
		glog.Errorf("unmarshal hosts failed: %s", err.Error())
		return err
	}
	upstream.UpdateHosts(hosts)

	// update scp
	reuseBuffer := viper.GetInt("scp.reuse_buffer")
	if reuseBuffer > 0 {
		scp.SentCacheSize = reuseBuffer
	}
	return
}

func main() {
	// set default log directory
	flag.Set("log_dir", "./")

	showVersion := flag.Bool("version", false, "show version and exit")
	testConfig := flag.Bool("t", false, "test configuration and exit")
	dumpConfig := flag.Bool("T", false, "test configuration, dump it and exit")
	configFile := flag.String("config", "./config.yaml", "set configuration file")
	flag.Parse()

	if *showVersion {
		fmt.Printf("goscon version: goscon/%s\n", _Version)
		os.Exit(0)
	}

	if *testConfig || *dumpConfig {
		if err := testConfigFile(*configFile); err != nil {
			fmt.Printf("read configuration file %s faield: %s\n", *configFile, err.Error())

			os.Exit(1)
		}

		fmt.Printf("the configuration file %s syntax is ok\n", *configFile)

		if *dumpConfig {
			fmt.Println(marshalConfigFile())
		}
		os.Exit(0)
	}

	viper.SetConfigFile(*configFile)
	if err := reloadConfig(); err != nil {
		os.Exit(1)
	}

	go handleSignal()

	var wg sync.WaitGroup

	// listen
	tcpListen := viper.GetString("tcp")
	if tcpListen != "" {
		l, err := NewTCPListener(tcpListen)
		if err != nil {
			glog.Errorf("tcp listen failed: addr=%s, err=%s", tcpListen, err.Error())
			os.Exit(1)
		}
		glog.Infof("tcp listen start: addr=%s", tcpListen)

		wg.Add(1)
		go func(l net.Listener) {
			defer l.Close()
			defer wg.Done()
			err := defaultServer.Serve(l)
			glog.Errorf("tcp listen stop: addr=%s, err=%s", tcpListen, err.Error())
		}(l)
	}

	kcpListen := viper.GetString("kcp")
	if kcpListen != "" {
		reuseport := viper.GetInt("kcp_option.reuseport")
		if reuseport <= 0 {
			reuseport = 1
		}
		for i := 0; i < reuseport; i++ {
			l, err := NewKCPListener(kcpListen)
			if err != nil {
				glog.Errorf("kcp listen failed: addr=%s, err=%s", kcpListen, err.Error())
				os.Exit(1)
			}
			glog.Infof("kcp listen start: addr=%s", kcpListen)

			wg.Add(1)
			go func(l net.Listener) {
				defer l.Close()
				defer wg.Done()
				err := defaultServer.Serve(l)
				glog.Errorf("kcp listen stop: addr=%s, err=%s", tcpListen, err.Error())
			}(l)
		}
	}
	wg.Wait()
	glog.Flush()
}
