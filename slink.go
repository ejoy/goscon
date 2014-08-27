//
//   date  : 2014-07-20 14:31
//   author: xjdrew
//
package main

import (
	"crypto/rc4"
	"errors"
	"net"
	"time"
)

type StableLink struct {
	id uint32

	// build
	secret uint64
	index  uint32

	// conn pair
	remote *net.TCPConn
	local  *net.TCPConn

	// data
	received uint32
	sent     uint32
	used     int
	cache    []byte

	// chan
	sendErrCh chan *net.TCPConn
	recvErrCh chan *net.TCPConn
	reuseCh   chan *ReuseConn

	workers int
	doneCh  chan bool

	//
	broken bool

	// rc4
	recvRc4 *rc4.Cipher
	sendRc4 *rc4.Cipher
}

// return value:
//    false: need break
func (s *StableLink) setSendErr(conn *net.TCPConn, err error) bool {
	if err != nil {
		Debug("link(%d) error, conn:%v, err:%v ", s.id, conn.RemoteAddr(), err)
	}
	s.sendErrCh <- conn

	if s.broken {
		return false
	}
	return true
}

func (s *StableLink) setRecvErr(conn *net.TCPConn, err error) bool {
	if err != nil {
		Debug("link(%d) error, conn:%v, err:%v ", s.id, conn.RemoteAddr(), err)
	}
	s.recvErrCh <- conn

	if s.broken {
		return false
	}
	return true
}

func (s *StableLink) forwardToLocal() {
	defer s.done()
	cache := make([]byte, 4096)
	remote, local := s.remote, s.local

	var n int
	var err error
	for {
		// pump from remote
		n, err = remote.Read(cache)
		if err != nil { // remote error
			if !s.setRecvErr(remote, err) {
				break
			}
		}

		if !s.setRecvErr(nil, nil) {
			break
		}
		// reuse point
		if remote != s.remote {
			Error("link(%d) drop data from remote, len:%d", s.id, n)
			remote = s.remote
			// drop read from old remote
			continue
		}

		if n == 0 {
			continue
		}

		// pour into local
		s.received += uint32(n)
		s.recvRc4.XORKeyStream(cache[:n], cache[:n])
		Debug("link(%d) forward to local, len:%d", s.id, n)
		_, err = local.Write(cache[:n])
		if err != nil { // local error, shoud close link
			s.setRecvErr(local, err)
			break
		}
	}
}

func (s *StableLink) forwardToRemote() {
	defer s.done()
	cache := make([]byte, 4096)
	remote, local := s.remote, s.local

	var n int
	var err error
	for {
		// pump from local
		n, err = local.Read(cache)
		if err != nil { // local error, shoud close link
			s.setSendErr(local, err)
			break
		}

		if !s.setSendErr(nil, nil) {
			break
		}

		// reuse point
		if remote != s.remote {
			remote = s.remote
		}

		// pour into remote
		// cache last sent
		s.sendRc4.XORKeyStream(cache[:n], cache[:n])

		s.sent += uint32(n)
		if s.used+n > cap(s.cache) {
			s.used = cap(s.cache) - n
			copy(s.cache, s.cache[n:])
		}
		copy(s.cache[s.used:], cache[:n])
		s.used += n

		_, err = remote.Write(cache[:n])
		if err != nil {
			if !s.setSendErr(remote, err) {
				break
			}
		} else {
			Debug("link(%d) forward to remote, len:%d", s.id, n)
		}
	}
}

func (s *StableLink) waitReuse() *ReuseConn {
	var errTime time.Time
	for {
		var rc *ReuseConn
		var conn *net.TCPConn
		if errTime.IsZero() {
			select {
			case conn = <-s.sendErrCh:
			case conn = <-s.recvErrCh:
			case rc = <-s.reuseCh:
				return rc
			}
		} else {
			now := time.Now()
			if errTime.Before(now) {
				Info("link(%d) wait reuse timeout", s.id)
				return nil
			}
			select {
			case conn = <-s.sendErrCh:
			case rc = <-s.reuseCh:
				return rc
			case <-time.After(errTime.Sub(now)):
				Info("link(%d) wait reuse timeout", s.id)
				return nil
			}
		}

		if conn == s.local { // local error
			return nil
		} else if conn == s.remote && errTime.IsZero() { // remote error
			Debug("link(%d) remote error, wait reuse", s.id)
			errTime = time.Now().Add(time.Second * time.Duration(options.Timeout))
		}
	}
}

func (s *StableLink) reuse(rc *ReuseConn) error {
	conn := rc.conn
	req := rc.req
	Info("link(%d) reuse conn:%v", s.id, conn.RemoteAddr())

	// index must be equal
	if req.index != s.index {
		Info("link(%d) reuse failed, index expired", s.id)
		conn.Close()
		return nil
	}

	var diff uint32
	if s.sent < req.received {
		diff = s.sent + (0xffffffff - req.received)
	} else {
		diff = s.sent - req.received
	}

	// set write resp timeout
	conn.SetWriteDeadline(time.Now().Add(time.Second))
	if diff > uint32(s.used) {
		Info("link(%d) reuse failed:%d", s.id, 406)
		WriteReuseConnResp(conn, 0, 406)
		conn.Close()
		return errors.New("406 buffer not enough")
	}

	err := WriteReuseConnResp(conn, s.received, 200)
	if err != nil {
		Error("link(%d) write reuse conn resp failed:%v", s.id, err.Error())
		conn.Close()
		return nil
	}

	// resend buffered
	if diff > 0 {
		Error("link(%d) resend buffer:%d", s.id, diff)
		from := uint32(s.used) - diff
		_, err = conn.Write(s.cache[from:s.used])
		if err != nil {
			Error("link(%d) resend buffer:%v", s.id, err.Error())
			conn.Close()
			return nil
		}
	}

	// everything is ok, reuse
	Info("link(%d) reuse succeed:%v -> %v", s.id, s.remote.RemoteAddr(), conn.RemoteAddr())
	s.remote.Close()
	s.remote = conn

	// cancle write timeout
	var t time.Time
	conn.SetWriteDeadline(t)
	return nil
}

func (s *StableLink) done() {
	s.doneCh <- true
}

// start forward
func (s *StableLink) Run() {
	s.workers = 1
	token, secret := GenToken(s.secret)
	s.secret = secret

	Info("link(%d) run, remote:%v, local:%v, secret:%x", s.id, s.remote.RemoteAddr(), s.local.RemoteAddr(), s.secret)
	err := WriteNewConnResp(s.remote, s.id, token)
	if err != nil {
		Error("link(%d) write new conn resp failed:%v", s.id, err.Error())
		return
	}

	key := make([]byte, 32)
	GenRC4Key(s.secret, 0, key[0:8])
	GenRC4Key(s.secret, 1, key[8:16])
	GenRC4Key(s.secret, 2, key[16:24])
	GenRC4Key(s.secret, 3, key[24:32])
	s.recvRc4, _ = rc4.NewCipher(key)
	s.sendRc4, _ = rc4.NewCipher(key)

	s.workers += 1
	go s.forwardToLocal()
	s.workers += 1
	go s.forwardToRemote()
	for {
		rc := s.waitReuse()
		if rc == nil {
			break
		}
		err := s.reuse(rc)
		if err != nil {
			break
		}
	}
	s.broken = true
}

func (s *StableLink) IsBroken() bool {
	return s.broken
}

func (s *StableLink) VerifyReuse(req *ReuseConnReq) uint32 {
	if s.index >= req.index {
		return 403
	}

	if !VerifySecret(s.secret, req) {
		return 401
	}

	// update index
	s.index = req.index
	return 200
}

func (s *StableLink) Reuse(rc *ReuseConn) {
	s.reuseCh <- rc
}

func (s *StableLink) StopReuse() {
	close(s.reuseCh)
}

func (s *StableLink) Wait() {
	s.remote.Close()
	s.local.Close()

	done := s.workers
	for {
		select {
		case <-s.sendErrCh:
		case <-s.recvErrCh:
			// do nothing
		case <-s.doneCh:
			done -= 1
		case _, ok := <-s.reuseCh:
			if !ok {
				done -= 1
				s.reuseCh = nil
			}
		}
		if done == 0 {
			break
		}
	}

	//
	close(s.sendErrCh)
	close(s.recvErrCh)
	close(s.doneCh)
	Info("link(%d) close", s.id)
}

func NewStableLink(id uint32, remote *net.TCPConn, local *net.TCPConn, key uint64) *StableLink {
	link := new(StableLink)

	link.id = id
	link.secret = key
	link.remote = remote
	link.local = local

	link.sendErrCh = make(chan *net.TCPConn)
	link.recvErrCh = make(chan *net.TCPConn)
	link.reuseCh = make(chan *ReuseConn)
	link.doneCh = make(chan bool)

	link.used = 0
	link.cache = make([]byte, options.SendBuf)

	link.broken = false
	return link
}
