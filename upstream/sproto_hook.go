// +build sproto

package upstream

import (
	"bytes"
	"encoding/binary"
	"flag"
	"net"
	"sync"

	"github.com/ejoy/goscon/scp"
	sproto "github.com/xjdrew/gosproto"
)

type sprotoPackage struct {
	Type    int32  `sproto:"integer,0,name=type"`
	Session *int32 `sproto:"integer,1,name=session"`
	Ud      int32  `sproto:"integer,2,name=ud"`
}

type sprotoAnnounceAddr struct {
	RemoteAddr string `sproto:"string,0,name=remote_addr"`
	LocalAddr  string `sproto:"string,1,name=local_addr"`
}

var optSproto int

var bufPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

type sprotoHook struct {
	mu         sync.Mutex
	packHeader []byte
}

func (s *sprotoHook) init() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.packHeader != nil {
		return
	}

	pack := &sprotoPackage{
		Type: int32(optSproto),
	}
	s.packHeader = sproto.MustEncode(pack)
}

func (s *sprotoHook) AfterConnected(local net.Conn, remote *scp.Conn) (err error) {
	if !flag.Parsed() {
		return
	}

	if optSproto == -1 {
		return
	}

	if remote.ForbidForwardIP() {
		return
	}

	if s.packHeader == nil {
		s.init()
	}

	// TODO: sproto.Pack allocate 0 memory
	aa := sproto.MustEncode(&sprotoAnnounceAddr{
		RemoteAddr: remote.RemoteAddr().String(),
		LocalAddr:  remote.LocalAddr().String(),
	})
	data := sproto.Pack(append(s.packHeader, aa...))

	buf := bufPool.Get().(*bytes.Buffer)
	defer bufPool.Put(buf)

	buf.Reset()
	binary.Write(buf, binary.BigEndian, uint16(len(data)))
	buf.Write(data)
	_, err = local.Write(buf.Bytes())
	return
}

func init() {
	flag.IntVar(&optSproto, "sproto", -1, "sproto message type")

	setHook(&sprotoHook{})
}
