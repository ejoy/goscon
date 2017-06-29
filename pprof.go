package main

import (
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
)

func init() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Print(err)
		return
	}
	log.Print("listen on ", ln.Addr().String())
	go func() {
		log.Print(http.Serve(ln, nil))
	}()
}
