//
//   date  : 2014-05-23 17:35
//   author: xjdrew
//

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/xjdrew/glog"
)

// log rule:
// verbose:
// 0, default, 确定数量的日志
// 1, connection related, 跟连接相关的日志， 一般每条连接在生命周期内产生x条
// 2, packet releated, 跟包相关的日志，一般每个packet都会产生数条日志

// <2019-10-18> 1.0.0: 重写goscon的配置方式
// <2019-12-24> 1.1.0: 优化内部设计，提升kcp可靠性等
var _Version = "1.1.0"

func testConfigFile(filename string) error {
	SetConfigFile(filename)
	return ReloadConfig()
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
			fmt.Println(MarshalConfigFile())
		}
		os.Exit(0)
	}

	SetConfigFile(*configFile)
	if err := ReloadConfig(); err != nil {
		os.Exit(1)
	}

	if err := startManager(GetConfigManager()); err != nil {
		glog.Errorf("start manager failed: err=%s", err.Error())
		os.Exit(1)
	}

	for _, server := range GetConfigServers() {
		option := GetConfigServerOption(server)
		if err := startServer(server, option); err != nil {
			glog.Errorf("listen failed: %s", err.Error())
			os.Exit(1)
		}
	}

	allServerDone()

	glog.Flush()
}
