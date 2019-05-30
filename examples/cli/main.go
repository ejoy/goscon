package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"

	//"fmt"
	"github.com/ejoy/goscon/scp"
	kcp "github.com/ejoy/kcp-go"
)

func DialWithOptions(network, connect string, fecData, fecParity int) (net.Conn, error) {
	if network == "tcp" {
		return net.Dial(network, connect)
	} else {
		return kcp.DialWithOptions(connect, nil, fecData, fecParity)
	}
}

func getOldScon(network, sent string, connect string, targetServer string, fecData, fecParity int) (*scp.Conn, error) {
	if sent == "" {
		return nil, nil
	}

	conn, err := DialWithOptions(network, connect, fecData, fecParity)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	scon := scp.Client(conn, &scp.Config{TargetServer: targetServer})
	if _, err = scon.Write([]byte(sent)); err != nil {
		return nil, err
	}
	return scon, nil
}

type stdoutFormater struct {
	*os.File
}

func (sf *stdoutFormater) Write(data []byte) (int, error) {
	//return fmt.Fprintf(sf.File, "% x", data)
	return sf.File.Write(data)
}

func main() {
	var sent, connect, targetServer string
	var fecData int
	var fecParity int
	var network string
	flag.StringVar(&connect, "connect", "127.0.0.1:1248", "connect to")
	flag.StringVar(&sent, "sent", "hello, world!\n", "sent")
	flag.StringVar(&targetServer, "targetServer", "", "target server")
	kcp := flag.NewFlagSet("kcp", flag.ExitOnError)
	kcp.IntVar(&fecData, "fec_data", 1, "FEC: number of shards to split the data into")
	kcp.IntVar(&fecParity, "fec_parity", 0, "FEC: number of parity shards")
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 && args[0] == "kcp" {
		kcp.Parse(args[1:])
		network = "kcp"
	} else {
		network = "tcp"
	}

	oldScon, err := getOldScon(network, sent, connect, targetServer, fecData, fecParity)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := DialWithOptions(network, connect, fecData, fecParity)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	scon := scp.Client(conn, &scp.Config{
		TargetServer:  targetServer,
		ConnForReused: oldScon,
	})

	if err := scon.Handshake(); err != nil {
		log.Fatal(err)
	}

	stdout := &stdoutFormater{os.Stdout}
	go io.Copy(stdout, scon)
	io.Copy(scon, os.Stdin)
}
