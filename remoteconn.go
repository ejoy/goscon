package main

import (
	"errors"
	"sync"
	"time"

	"github.com/ejoy/goscon/scp"
)

var errConnClosed = errors.New("conn closed")

// SCPConn .
type SCPConn struct {
	*scp.Conn

	connMutex  sync.Mutex
	connCond   *sync.Cond
	connErr    error // error when operate on conn
	connClosed bool  // conn closed

	// for reuse timeout
	reuseCh      chan struct{}
	reuseTimeout time.Duration
}

func (s *SCPConn) setConnError(conn *scp.Conn, err error) {
	if err == nil {
		return
	}

	s.connMutex.Lock()
	defer s.connMutex.Unlock()
	if conn != s.Conn {
		return
	}

	if s.connClosed {
		return
	}
	s.connErr = err
}

// startWait 启动超时计数
func (s *SCPConn) startWait() {
	if s.reuseCh != nil {
		return
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-time.After(s.reuseTimeout):
			s.Close()
		case <-done:
		}
	}()
	s.reuseCh = done
}

// stopWait 停止超时计数
func (s *SCPConn) stopWait() {
	if s.reuseCh == nil {
		return
	}
	close(s.reuseCh)
	s.reuseCh = nil
}

func (s *SCPConn) acquireConn() (*scp.Conn, error) {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()
	for {
		if s.connClosed {
			return nil, s.connErr
		} else if s.connErr != nil {
			s.startWait()
			s.connCond.Wait()
			s.stopWait()
		} else {
			return s.Conn, nil
		}
	}
}

// Read .
func (s *SCPConn) Read(p []byte) (int, error) {
	conn, err := s.acquireConn()
	if err != nil {
		return 0, err
	}

	n, err := conn.Read(p)
	if err != nil {
		// freeze, waiting for reuse
		conn.Freeze()
		s.setConnError(conn, err)
	}
	return n, nil
}

// Write returns until succeed or Conn is closed
func (s *SCPConn) Write(p []byte) (int, error) {
	var nn int
	for {
		conn, err := s.acquireConn()
		if err != nil { // conn is closed
			return 0, err
		}
		n, err := conn.Write(p[nn:])

		if err != nil {
			// freeze, waiting for reuse
			conn.Freeze()
			s.setConnError(conn, err)
		}
		nn = nn + n
		if nn == len(p) {
			break
		}
	}
	return nn, nil
}

// ReplaceConn .
func (s *SCPConn) ReplaceConn(conn *scp.Conn) bool {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()
	if s.connClosed {
		return false
	}

	// TODO: check s.Conn and conn is match

	// close old conn
	s.Conn.Close()

	// set new status
	s.Conn = conn
	s.connErr = nil
	s.connCond.Broadcast()
	return true
}

// Close .
func (s *SCPConn) Close() error {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()
	if s.connClosed {
		return s.connErr
	}

	s.connClosed = true
	s.connErr = errConnClosed
	err := s.Conn.Close()
	s.connCond.Broadcast()
	return err
}

// NewSCPConn .
func NewSCPConn(scon *scp.Conn, option *SCPOption) *SCPConn {
	scpConn := &SCPConn{Conn: scon}
	scpConn.connCond = sync.NewCond(&scpConn.connMutex)
	scpConn.reuseTimeout = option.ReuseTime * time.Second
	return scpConn
}
