package main

import (
	"bufio"
	"flag"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	//"os"
	//"fmt"
	"github.com/ejoy/goscon/scp"
	"github.com/xtaci/kcp-go"
)

func DialWithOptions(network, connect string, fecData, fecParity int) (net.Conn, error) {
	if network == "tcp" {
		return net.Dial(network, connect)
	} else {
		return kcp.DialWithOptions(connect, nil, fecData, fecParity)
	}
}

func DialScon(network, connect string, targetServer string, fecData, fecParity int) (*scp.Conn, error) {
	conn, err := DialWithOptions(network, connect, fecData, fecParity)
	if err != nil {
		return nil, err
	}

	scon := scp.Client(conn, &scp.Config{TargetServer: targetServer})
	return scon, nil
}

func bench(conn net.Conn, host string, payload string) {
	req, err := http.NewRequest("GET", "http://"+host+"/", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Add("Connection", "keep-alive")
	req.Header.Add("Payload", payload)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	writer := bufio.NewWriter(conn)

	round := 0
	slow := 0
	for range ticker.C {
		start := time.Now()

		err = req.Write(writer)
		if err != nil {
			log.Printf("write err:%s", err)
			break
		}
		writer.Flush()

		reader := bufio.NewReader(conn)
		resp, err := http.ReadResponse(reader, nil)
		if err != nil {
			log.Printf("read err:%s", err)
			break
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			log.Printf("http err: %s", resp.Status)
			break
		}

		elapsed := time.Now().Sub(start)

		round++
		if elapsed > time.Millisecond*10 {
			slow++
		}

		if round%100 == 0 {
			log.Printf("Round %d, slow: %d", round, slow)
		}
	}
}

func main() {
	var connect, targetServer string
	var fecData int
	var fecParity int
	var network string
	var payload int
	flag.StringVar(&connect, "connect", "127.0.0.1:1248", "connect to")
	flag.StringVar(&targetServer, "targetServer", "", "target server")
	kcp := flag.NewFlagSet("kcp", flag.ExitOnError)
	kcp.IntVar(&fecData, "fec_data", 1, "FEC: number of shards to split the data into")
	kcp.IntVar(&fecParity, "fec_parity", 0, "FEC: number of parity shards")

	flag.IntVar(&payload, "payload", 0, "http test payload size")
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 && args[0] == "kcp" {
		kcp.Parse(args[1:])
		network = "kcp"
	} else {
		network = "tcp"
	}

	sconn, err := DialScon(network, connect, targetServer, fecData, fecParity)
	if err != nil {
		log.Fatal(err)
	}

	bench(sconn, connect, strings.Repeat("a", payload))
}
