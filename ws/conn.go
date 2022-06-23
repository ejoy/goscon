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
	frameSize   int
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

	if err != nil {
		return 0, err
	}

	if frameType != c.messageType {
		return 0, errors.New("Invalid message type")
	}

	c.reader = reader

	return reader.Read(b)
}

func (c *Conn) Write(b []byte) (int, error) {
	frameSize := c.frameSize
	buffSize := len(b)
	frameCount := buffSize / frameSize
	sendBytes := 0

	for i := 0; i < frameCount; i += 1 {
		err := c.WriteMessage(websocket.BinaryMessage, b[:frameSize])
		if err != nil {
			return sendBytes, err
		}

		b = b[frameSize:]
		sendBytes += frameSize
	}

	leftBytes := len(b)
	if leftBytes == 0 {
		return sendBytes, nil
	}

	err := c.WriteMessage(websocket.BinaryMessage, b)
	if err == nil {
		sendBytes += leftBytes
	}

	return sendBytes, err
}

func (c *Conn) SetDeadline(t time.Time) error {
	err := c.SetReadDeadline(t)
	if err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func NewConn(c *websocket.Conn, messageType, frameSize int, readTimeout time.Duration) *Conn {
	return &Conn{
		Conn:        c,
		messageType: messageType,
		frameSize:   frameSize,
		readTimeout: readTimeout,
	}
}

func Dial(host string) (net.Conn, error) {
	c, _, err := websocket.DefaultDialer.DialContext(context.Background(), host, nil)
	if err != nil {
		return nil, err
	}

	return NewConn(c, 0, 4096, websocket.BinaryMessage), nil
}
