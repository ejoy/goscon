// +build debug

package main

import (
	_ "net/http/pprof"

	"github.com/golang/glog"
)

func init() {
	glog.Info("pprof enabled")
}
