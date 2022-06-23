package main

import (
	"net"
	"net/http"

	"github.com/ejoy/goscon/ws"
	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
	"github.com/xjdrew/glog"
)

type wsHandler struct {
	upgrader *websocket.Upgrader
	connChan chan *websocket.Conn
}

type WSListener struct {
	server   *http.Server
	connChan chan *websocket.Conn
}

func (h *wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		glog.Error(err)
		return
	}

	h.connChan <- c
}

func (l *WSListener) Accept() (net.Conn, error) {
	c := ws.NewConn(<-l.connChan, websocket.BinaryMessage,
		configItemTime("ws_option.read_timeout"))

	if glog.V(1) {
		glog.Infof("accept new ws connection: addr=%s", c.RemoteAddr())
	}

	return c, nil
}

func (l *WSListener) Close() error {
	return l.server.Close()
}

func (l *WSListener) Addr() net.Addr {
	addr, _ := net.ResolveTCPAddr("tcp", l.server.Addr)
	return addr
}

func NewWSListener(addr string) (*WSListener, error) {
	backlog := configItemInt("ws_option.backlog")
	connChan := make(chan *websocket.Conn, backlog)

	upgrader := &websocket.Upgrader{
		EnableCompression: true,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	server := &http.Server{
		Addr:    addr,
		Handler: &wsHandler{upgrader, connChan},
	}

	go func() {
		certFile := viper.GetString("ws_option.cert_file")
		keyFile := viper.GetString("ws_option.key_file")

		if certFile == "" || keyFile == "" {
			server.ListenAndServe()
		} else {
			server.ListenAndServeTLS(certFile, keyFile)
		}
	}()

	return &WSListener{server, connChan}, nil
}
