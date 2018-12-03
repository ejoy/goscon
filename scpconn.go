package main

import (
	"errors"
	"sync"
	"time"

	"github.com/ejoy/goscon/scp"
)

var errConnClosed = errors.New("conn closed")

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
			go func() {
				select {
				case <-time.After(s.reuseTimeout):
					s.Close()
				case <-s.reuseCh:
				}
			}()
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

func (s *SCPConn) Write(p []byte) (int, error) {
	err := s.lockIfNoError(&s.wr)
	if err != nil {
		return 0, err
	}
	defer s.wr.Unlock()

	n, err := s.Conn.Write(p)
	if err != nil {
		s.closeWrite()
		s.setError(err)
	}
	return n, nil
}

func (s *SCPConn) setClosed() {
	if !s.connClosed {
		s.connMutex.Lock()
		s.connClosed = true
		s.connErr = errConnClosed
		s.connCond.Broadcast()
		s.connMutex.Unlock()
	}
}

// set new conn
func (s *SCPConn) SetConn(conn *scp.Conn) {
	s.connMutex.Lock()
	defer s.connMutex.Unlock()
	if s.connClosed {
		return
	}

	if s.connErr == nil {
		panic("s.connErr == nil")
	}

	// not reading
	s.rd.Lock()
	s.rd.Unlock()

	// not writing
	s.wr.Lock()
	s.wr.Unlock()

	s.Conn = conn
	s.setErrorWithLocked(nil)
	s.connCond.Broadcast()
}

// close low-level conn and wait for reuse
func (s *SCPConn) CloseForReuse() {
	s.setError(errConnClosed)
	s.Conn.Close()

	// not reading
	s.rd.Lock()
	s.rd.Unlock()

	// not writing
	s.wr.Lock()
	s.wr.Unlock()
}

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

func (s *SCPConn) CloseRead() error {
	s.setClosed()
	return s.closeRead()
}

func (s *SCPConn) CloseWrite() error {
	s.setClosed()
	return s.closeWrite()
}

func (s *SCPConn) RawConn() *scp.Conn {
	return s.Conn
}

func NewSCPConn(scon *scp.Conn, resueTimeout time.Duration) *SCPConn {
	scpConn := &SCPConn{Conn: scon}
	scpConn.connCond = sync.NewCond(&scpConn.connMutex)
	scpConn.reuseTimeout = resueTimeout
	return scpConn
}
