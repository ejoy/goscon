package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"runtime"

	"github.com/golang/glog"
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

	http.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		status := make(map[string]interface{})
		status["procs"] = runtime.GOMAXPROCS(0)
		status["num_of_cpu"] = runtime.NumCPU()
		status["goroutines"] = runtime.NumGoroutine()
		status["pairs"] = defaultServer.NumOfConnPairs()
		enc := json.NewEncoder(w)
		enc.Encode(status)
	})

	go func() {
		defer ln.Close()
		err := http.Serve(ln, nil)
		glog.Errorf("manager exit: err=%v", err)
	}()
	return nil
}
