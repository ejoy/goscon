package sproto

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

var (
	ErrRepeatedRpc     = errors.New("sproto rpc: repeated rpc")
	ErrUnknownProtocol = errors.New("sproto rpc: unknown protocol")
	ErrUnknownSession  = errors.New("sproto rpc: unknown session")
)

type RpcMode int

const (
	RpcRequestMode RpcMode = iota
	RpcResponseMode
)

type rpcHeader struct {
	Type    *int32 `sproto:"integer,0,name=type"`
	Session *int32 `sproto:"integer,1,name=session"`
}

type Protocol struct {
	Type       int32
	Name       string
	MethodName string
	Request    reflect.Type
	Response   reflect.Type
}

func (p *Protocol) HasRequest() bool {
	return p.Request != nil
}

func (p *Protocol) HasResponse() bool {
	return p.Response != nil
}

// func (rcvr Reciver) MethodName(req protocol.Request, response protocol.Response)
func (p *Protocol) MatchMethod(method reflect.Method) error {
	mtyp := method.Type

	// default args: rcvr
	numIn := 1
	if p.Request != nil {
		numIn += 1
	}

	if p.Response != nil {
		numIn += 1
	}

	if mtyp.NumIn() != numIn || mtyp.NumOut() != 0 {
		return fmt.Errorf("sproto: method %s should have %d arguments and 0 return values", p.MethodName, numIn)
	}

	if p.Request != nil {
		if mtyp.In(1) != p.Request {
			return fmt.Errorf("sproto: method %s arg%d should be %s", p.MethodName, 1, p.Request.String())
		}
	}

	if p.Response != nil {
		if mtyp.In(numIn-1) != p.Response {
			return fmt.Errorf("sproto: method %s arg%d should be %s", p.MethodName, 2, p.Response.String())
		}
	}
	return nil
}

type Rpc struct {
	protocols    []*Protocol
	idMap        map[int32]int
	nameMap      map[string]int
	methodMap    map[string]int
	sessionMutex sync.Mutex
	sessions     map[int32]int
}

func getRpcSprotoType(typ reflect.Type) (*SprotoType, error) {
	if typ == nil {
		return nil, nil
	}

	if typ.Kind() != reflect.Ptr {
		return nil, ErrNonPtr
	}

	return GetSprotoType(typ.Elem())
}

func (rpc *Rpc) Dispatch(packed []byte) (mode RpcMode, name string, session int32, sp interface{}, err error) {
	var unpacked []byte
	if unpacked, err = Unpack(packed); err != nil {
		return
	}

	var used int
	header := rpcHeader{}
	if used, err = Decode(unpacked, &header); err != nil {
		return
	}

	var proto *Protocol
	if header.Type != nil {
		index, ok := rpc.idMap[*header.Type]
		if !ok {
			err = ErrUnknownProtocol
			return
		}
		proto = rpc.protocols[index]
		if proto.Request != nil {
			sp = reflect.New(proto.Request.Elem()).Interface()
			if _, err = Decode(unpacked[used:], sp); err != nil {
				return
			}
		}
		mode = RpcRequestMode
		if header.Session != nil {
			session = *header.Session
		}
	} else {
		if header.Session == nil {
			err = ErrUnknownSession
			return
		}
		session = *header.Session
		rpc.sessionMutex.Lock()
		defer rpc.sessionMutex.Unlock()
		index, ok := rpc.sessions[session]
		if !ok {
			err = ErrUnknownSession
			return
		}
		delete(rpc.sessions, session)

		proto = rpc.protocols[index]
		if proto.Response != nil {
			sp = reflect.New(proto.Response.Elem()).Interface()
			if _, err = Decode(unpacked[used:], sp); err != nil {
				return
			}
		}
		mode = RpcResponseMode
	}
	name = proto.Name
	return
}

func (rpc *Rpc) ResponseEncode(name string, session int32, response interface{}) (data []byte, err error) {
	index, ok := rpc.nameMap[name]
	if !ok {
		err = ErrUnknownProtocol
		return
	}

	protocol := rpc.protocols[index]
	if protocol.Response != nil {
		if data, err = Encode(response); err != nil {
			return
		}
	}

	header, _ := Encode(&rpcHeader{Session: &session})
	data = Pack(Append(header, data))
	return
}

// session > 0: need response
func (rpc *Rpc) RequestEncode(name string, session int32, req interface{}) (data []byte, err error) {
	index, ok := rpc.nameMap[name]
	if !ok {
		err = ErrUnknownProtocol
		return
	}

	protocol := rpc.protocols[index]
	if protocol.Request != nil {
		if data, err = Encode(req); err != nil {
			return
		}
	}

	header := &rpcHeader{
		Type: &protocol.Type,
	}

	if protocol.HasResponse() {
		rpc.sessionMutex.Lock()
		defer rpc.sessionMutex.Unlock()
		if _, ok := rpc.sessions[session]; ok {
			err = fmt.Errorf("sproto: repeated session:%d", session)
			return
		}
		header.Session = &session
		rpc.sessions[session] = index
	}

	chunk, _ := Encode(header)
	data = Pack(Append(chunk, data))
	return
}

// get protocol by method name
func (rpc *Rpc) GetProtocolByMethod(method string) *Protocol {
	if index, ok := rpc.methodMap[method]; ok {
		return rpc.protocols[index]
	}
	return nil
}

// get protocol by name
func (rpc *Rpc) GetProtocolByName(name string) *Protocol {
	if index, ok := rpc.nameMap[name]; ok {
		return rpc.protocols[index]
	}
	return nil
}

func NewRpc(protocols []*Protocol) (*Rpc, error) {
	idMap := make(map[int32]int)
	nameMap := make(map[string]int)
	methodMap := make(map[string]int)
	for i, protocol := range protocols {
		if _, err := getRpcSprotoType(protocol.Request); err != nil {
			return nil, err
		}
		if _, err := getRpcSprotoType(protocol.Response); err != nil {
			return nil, err
		}
		if _, ok := idMap[protocol.Type]; ok {
			return nil, ErrRepeatedRpc
		}
		if _, ok := nameMap[protocol.Name]; ok {
			return nil, ErrRepeatedRpc
		}
		if _, ok := methodMap[protocol.MethodName]; ok {
			return nil, ErrRepeatedRpc
		}
		idMap[protocol.Type] = i
		nameMap[protocol.Name] = i
		methodMap[protocol.MethodName] = i
	}
	rpc := &Rpc{
		protocols: protocols,
		idMap:     idMap,
		nameMap:   nameMap,
		methodMap: methodMap,
		sessions:  make(map[int32]int),
	}
	return rpc, nil
}
