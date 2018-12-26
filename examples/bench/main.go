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

type Stat struct {
	conn    int
	slow    int
	round   int
	percent map[int]int
}

var statInterval []int
var statGap int
var statLevel int

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

func bench(i int, conn net.Conn, host string, payload string, chStat chan Stat) {
	req, err := http.NewRequest("GET", "http://"+host+"/", nil)
	if err != nil {
		panic(err)
	}
	req.Header.Add("Connection", "keep-alive")
	req.Header.Add("Payload", payload)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	writer := bufio.NewWriter(conn)

	stat := Stat{conn: i, slow: 0, round: 0, percent: make(map[int]int)}
	for _, bound := range statInterval {
		stat.percent[bound] = 0
	}

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
		resp.Body.Close()

		if resp.StatusCode != 200 {
			log.Printf("http err: %s", resp.Status)
			break
		}

		elapsed := time.Now().Sub(start)

		stat.round++
		if elapsed > 10*time.Millisecond {
			stat.slow++
		}
		idx := int(elapsed / time.Millisecond)
		if idx < statLevel {
			stat.percent[statInterval[idx]]++
		} else {
			stat.percent[0xFFFFFFFF]++
		}

		if stat.round%100 == 0 {
			chStat <- stat
		}
	}
}

func main() {
	var connect, targetServer string
	var connections int
	var payload int
	var fecData int
	var fecParity int
	var network string
	flag.StringVar(&connect, "connect", "127.0.0.1:1248", "connect to")
	flag.StringVar(&targetServer, "targetServer", "", "target server")
	flag.IntVar(&connections, "connections", 1, "concurrent connections")
	flag.IntVar(&statGap, "statGap", 10, "stat milliseconds per gap")
	flag.IntVar(&statLevel, "statLevel", 10, "stat level")
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

	if statLevel < 1 {
		statLevel = 1
	}

	if statGap < 1 {
		statGap = 1
	}
	statInterval = make([]int, statLevel + 1)

	for i := 0; i < statLevel; i++ {
		statInterval[i] = (i + 1) * statGap
	}
	statInterval[statLevel] = 0xFFFFFFFF

	chStat := make(chan Stat, 4*connections)
	for i := 0; i < connections; i++ {
		idx := i + 1
		go func() {
			sconn, err := DialScon(network, connect, targetServer, fecData, fecParity)
			if err != nil {
				log.Fatal(err)
			}
			bench(idx, sconn, connect, strings.Repeat("a", payload), chStat)
		}()
	}

	for stat := range chStat {
		log.Printf("======== stat result on conn: %d", stat.conn)
		log.Printf(">>>> slow: %d, round: %d, slow rate: %.3f", stat.slow, stat.round, float32(stat.slow)/float32(stat.round))
		log.Printf(">>>> distribution")
		for i := 0; i < statLevel; i++ {
			log.Printf("bound (%d, %d]: %d", i * statGap, (i + 1) * statGap, stat.percent[statInterval[i]])
		}
		log.Printf("bound (%d, inf]: %d", statLevel * statGap, stat.percent[0xFFFFFFFF])
	}
}
