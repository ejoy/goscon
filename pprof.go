// +build debug

package main

import (
	_ "net/http/pprof"

	"github.com/xjdrew/glog"
)

func init() {
	glog.Info("pprof enabled")
}
