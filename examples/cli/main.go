package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	//"fmt"

	"github.com/ejoy/goscon/scp"
)

func getOldScon(sent string, connect string, targetServer string) (*scp.Conn, error) {
	if sent == "" {
		return nil, nil
	}

	conn, err := net.Dial("tcp", connect)
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
	flag.StringVar(&connect, "connect", "127.0.0.1:1248", "connect to")
	flag.StringVar(&sent, "sent", "hello, world!\n", "sent")
	flag.StringVar(&targetServer, "targetServer", "", "target server")
	flag.Parse()

	oldScon, err := getOldScon(sent, connect, targetServer)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.Dial("tcp", connect)
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
