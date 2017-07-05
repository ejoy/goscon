// +build sproto

package main

import (
	"encoding/binary"
	"flag"
	"io"
	"net"

	"github.com/xjdrew/gosproto"
)

type sprotoPackage struct {
	Type    int32 `sproto:"integer,0,name=type"`
	Session int32 `sproto:"integer,1,name=session"`
	Ud      int32 `sproto:"integer,2,name=ud"`
}

type sprotoAnnounceAddr struct {
	RemoteAddr string `sproto:"string,0,name=remote_addr"`
	LocalAddr  string `sproto:"string,1,name=local_addr"`
}

type SprotoConnWrapper struct {
	writer  func(wr io.Writer, data []byte) (int, error)
	msgType int32
}

func (scw *SprotoConnWrapper) Wrapper(local *net.TCPConn, remote net.Conn) (*net.TCPConn, error) {
	aa := &sprotoAnnounceAddr{
		RemoteAddr: remote.RemoteAddr().String(),
		LocalAddr:  remote.LocalAddr().String(),
	}

	pack := &sprotoPackage{
		Type: scw.msgType,
	}

	c1 := sproto.MustEncode(pack)
	c2 := sproto.MustEncode(aa)

	data := sproto.Pack(append(c1, c2...))
	_, err := scw.writer(local, data)
	if err != nil {
		return nil, err
	}
	return local, nil
}

func newSprotoConnWrapper(format string, msgType int) *SprotoConnWrapper {
	scw := &SprotoConnWrapper{}
	switch len(format) {
	case 0:
		format = "BS"
	case 1:
		format = format + "S"
	}

	var byteOrder binary.ByteOrder
	if format[0] == 'B' {
		byteOrder = binary.BigEndian
	} else {
		byteOrder = binary.LittleEndian
	}

	scw.writer = func(wr io.Writer, data []byte) (n int, err error) {
		sz := len(data)
		if format[1] == 'S' {
			if err = binary.Write(wr, byteOrder, uint16(sz)); err != nil {
				return
			}
		} else {
			if err = binary.Write(wr, byteOrder, uint32(sz)); err != nil {
				return
			}
		}
		n, err = wr.Write(data)
		return
	}

	scw.msgType = int32(msgType)
	return scw
}

var optSprotoWrapper bool
var optSprotoWrpperFormat string
var optSprotoMessageType int

func sprotoConnWrapperHook(provier *LocalConnProvider) {
	if !optSprotoWrapper {
		return
	}
	wrapper := newSprotoConnWrapper(optSprotoWrpperFormat, optSprotoMessageType)
	provier.MustSetWrapper(wrapper)
}

func init() {
	flag.BoolVar(&optSprotoWrapper, "sproto", false, "sproto wrapper")
	flag.StringVar(&optSprotoWrpperFormat, "sprotoFormat", "BS", "sproto header format(B/b, S:L)")
	flag.IntVar(&optSprotoMessageType, "sprotoMessageType", 3, "sproto message type")
	installWrapperHook(sprotoConnWrapperHook)
}
