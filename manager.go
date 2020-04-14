package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xjdrew/glog"
	"github.com/xtaci/kcp-go"
)

func startManager(laddr string) (err error) {
	if laddr == "" {
		return
	}
	ln, err := net.Listen("tcp", laddr)
	if err != nil {
		glog.Infof("start manager failed: listen=%s, err=%s", laddr, err.Error())
		return
	}
	glog.Infof("start manager: listen=%s", laddr)

	http.HandleFunc("/config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Content-Type", "text/vnd.yaml")
		io.WriteString(w, marshalConfigFile())
	})

	http.HandleFunc("/reload", func(w http.ResponseWriter, _ *http.Request) {
		err := reloadConfig()
		if err == nil {
			io.WriteString(w, "succeed")
		} else {
			io.WriteString(w, "failed: "+err.Error())
		}
	})

	http.HandleFunc("/kcp/snmp", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.Encode(kcp.DefaultSnmp.Copy())
	})

	http.Handle("/metrics", promhttp.Handler())

	go func() {
		defer ln.Close()
		err := http.Serve(ln, nil)
		glog.Errorf("manager exit: err=%v", err)
	}()
	return nil
}
