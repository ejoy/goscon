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

	rd, wr sync.Mutex

	connMutex  sync.Mutex
	connCond   *sync.Cond
	connErr    error // error when operate on conn
	connClosed bool  // conn closed

	reuseCh      chan struct{}
	reuseTimeout time.Duration
}

type closeWriter interface {
	CloseWrite() error
}

type closeReader interface {
	CloseRead() error
}

func (s *SCPConn) setErrorWithLocked(err error) {
	if err == nil {
		if s.connErr != nil {
			if s.reuseCh != nil {
				close(s.reuseCh)
				s.reuseCh = nil
			}
			s.connErr = nil
		}
	} else {
		if s.connErr == nil {
			if s.reuseCh != nil {
				panic(s.reuseCh != nil)
			}

			s.reuseCh = make(chan struct{})
			go func(reuseCh <-chan struct{}) {
				select {
				case <-time.After(s.reuseTimeout):
					s.Close()
				case <-reuseCh:
				}
			}(s.reuseCh)
			s.connErr = err
		}
	}
}

func (s *SCPConn) setError(err error) {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()
	s.setErrorWithLocked(err)
}

func (s *SCPConn) lockIfNoError(mutex *sync.Mutex) error {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()
	for {
		if s.connClosed {
			return s.connErr
		}
		if s.connErr != nil {
			s.connCond.Wait()
		} else {
			mutex.Lock()
			return nil
		}
	}
}

func (s *SCPConn) Read(p []byte) (int, error) {
	err := s.lockIfNoError(&s.rd)
	if err != nil {
		return 0, err
	}
	defer s.rd.Unlock()

	n, err := s.Conn.Read(p)
	if err != nil {
		s.closeRead()
		s.setError(err)
	}
	return n, nil
}

// Write until succeed or Conn is closed
func (s *SCPConn) Write(p []byte) (int, error) {
	var nn int
	for {
		err := s.lockIfNoError(&s.wr)
		if err != nil { // conn is closed
			return 0, err
		}
		n, err := s.Conn.Write(p[nn:])
		s.wr.Unlock()

		if err != nil {
			s.closeWrite()
			s.setError(err)
		}
		nn = nn + n
		if nn == len(p) {
			break
		}
	}
	return nn, nil
}

func (s *SCPConn) setClosed() {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()
	if s.connClosed {
		return
	}

	s.connClosed = true
	s.connErr = errConnClosed
	s.connCond.Broadcast()
}

// SetConn .
func (s *SCPConn) SetConn(conn *scp.Conn) bool {
	// check
	if conn.ID() != s.Conn.ID() {
		panic("conn.ID() != s.Conn.ID()")
	}

	// close old conn
	s.Conn.Close()

	// not reading
	s.rd.Lock()
	s.rd.Unlock()

	// not writing
	s.wr.Lock()
	s.wr.Unlock()

	s.connMutex.Lock()
	defer s.connMutex.Unlock()
	if s.connClosed {
		return false
	}
	s.Conn = conn
	s.setErrorWithLocked(nil)
	s.connCond.Broadcast()
	return true
}

// Close .
func (s *SCPConn) Close() error {
	s.setClosed()
	return s.Conn.Close()
}

func (s *SCPConn) closeWrite() error {
	if tcpConn, ok := s.Conn.RawConn().(closeWriter); ok {
		return tcpConn.CloseWrite()
	}
	return s.Conn.Close()
}

func (s *SCPConn) closeRead() error {
	if tcpConn, ok := s.Conn.RawConn().(closeReader); ok {
		return tcpConn.CloseRead()
	}
	return s.Conn.Close()
}

// CloseRead .
func (s *SCPConn) CloseRead() error {
	s.setClosed()
	return s.closeRead()
}

// CloseWrite .
func (s *SCPConn) CloseWrite() error {
	s.setClosed()
	return s.closeWrite()
}

// RawConn .
func (s *SCPConn) RawConn() *scp.Conn {
	return s.Conn
}

// NewSCPConn .
func NewSCPConn(scon *scp.Conn) *SCPConn {
	scpConn := &SCPConn{Conn: scon}
	scpConn.connCond = sync.NewCond(&scpConn.connMutex)
	scpConn.reuseTimeout = configItemTime("scp.reuse_time")
	return scpConn
}
