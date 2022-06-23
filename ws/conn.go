package ws

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/gorilla/websocket"
)

type Conn struct {
	*websocket.Conn
	messageType int
	readTimeout time.Duration
	reader      io.Reader
}

func (c *Conn) Read(b []byte) (int, error) {
	reader := c.reader

	if reader != nil {
		n, err := reader.Read(b)
		if n > 0 {
			return n, err
		}

		if err != nil && err != io.EOF {
			return 0, err
		}

		c.reader = nil
	}

	if c.readTimeout > 0 {
		c.SetReadDeadline(time.Now().Add(c.readTimeout))
	}

	frameType, reader, err := c.NextReader()

	if frameType != c.messageType {
		return 0, errors.New("Invalid message type")
	}

	if err != nil {
		return 0, err
	}

	c.reader = reader

	return reader.Read(b)
}

func (c *Conn) Write(b []byte) (int, error) {
	err := c.WriteMessage(websocket.BinaryMessage, b)
	return len(b), err
}

func (c *Conn) SetDeadline(t time.Time) error {
	err := c.SetReadDeadline(t)
	if err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func NewConn(c *websocket.Conn, messageType int, readTimeout time.Duration) *Conn {
	return &Conn{
		Conn:        c,
		messageType: messageType,
		readTimeout: readTimeout,
	}
}

func Dial(host string) (net.Conn, error) {
	c, _, err := websocket.DefaultDialer.DialContext(context.Background(), host, nil)
	if err != nil {
		return nil, err
	}

	return NewConn(c, 0, websocket.BinaryMessage), nil
}
