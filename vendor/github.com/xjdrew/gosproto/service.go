package sproto

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"reflect"
	"sync"
	"sync/atomic"
)

const (
	MSG_MAX_LEN = 0xffff
)

type OnUnknownPacket func(mode RpcMode, name string, session int32, sp interface{}) error

func defaultOnUnknownPacket(mode RpcMode, name string, session int32, sp interface{}) error {
	return fmt.Errorf("sproto: unknown packet, mode:%d, name:%s, session:%d", mode, name, session)
}

type Method struct {
	rcvr     reflect.Value
	method   reflect.Method
	protocol *Protocol
}

func (m *Method) call(req interface{}) interface{} {
	var resp reflect.Value
	in := make([]reflect.Value, m.method.Type.NumIn())
	in[0] = m.rcvr
	if m.protocol.HasRequest() {
		in[1] = reflect.ValueOf(req)
	}
	if m.protocol.HasResponse() {
		resp = reflect.New(m.protocol.Response.Elem())
		in[len(in)-1] = resp
	}
	m.method.Func.Call(in)
	if resp.IsValid() {
		return resp.Interface()
	}
	return nil
}

type Call struct {
	protocol *Protocol
	Resp     interface{}
	Err      error
	Done     chan *Call
}

func (call *Call) done() {
	select {
	case call.Done <- call:
	default:
		log.Panicf("sproto: method %s block", call.protocol.MethodName)
	}
}

type Service struct {
	rpc          *Rpc
	readMutex    sync.Mutex // gates read one at a time
	writeMutex   sync.Mutex // gates write one at a time
	rw           io.ReadWriter
	rdbuf        []byte // read buffer
	wrbuf        []byte // write buffer
	session      int32
	methodMutex  sync.Mutex
	methods      map[string]*Method
	sessionMutex sync.Mutex
	sessions     map[int32]*Call
	onUnknown    OnUnknownPacket
}

func (s *Service) nextSession() int32 {
	return atomic.AddInt32(&s.session, 1)
}

func (s *Service) setSession(session int32, call *Call) {
	s.sessionMutex.Lock()
	s.sessions[session] = call
	s.sessionMutex.Unlock()
}

func (s *Service) grabSession(session int32) *Call {
	s.sessionMutex.Lock()
	defer s.sessionMutex.Unlock()
	if call, ok := s.sessions[session]; ok {
		delete(s.sessions, session)
		return call
	}
	return nil
}

func (s *Service) setMethod(name string, method *Method) error {
	s.methodMutex.Lock()
	defer s.methodMutex.Unlock()

	if _, ok := s.methods[name]; ok {
		return fmt.Errorf("sproto:service %s has already registered", name)
	}
	s.methods[name] = method
	return nil
}

func (s *Service) getMethod(name string) *Method {
	s.methodMutex.Lock()
	defer s.methodMutex.Unlock()
	method := s.methods[name]
	return method
}

func (s *Service) getProtocol(module, method string) *Protocol {
	return s.rpc.GetProtocolByMethod(fmt.Sprintf("%s.%s", module, method))
}

func (s *Service) register(rcvr reflect.Value, typ reflect.Type) error {
	module := reflect.Indirect(rcvr).Type().Name()
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		protocol := s.getProtocol(module, method.Name)
		if protocol == nil {
			return fmt.Errorf("sproto:unknown service %s.%s", module, method.Name)
		}

		if err := protocol.MatchMethod(method); err != nil {
			return err
		}

		meth := &Method{
			rcvr:     rcvr,
			method:   method,
			protocol: protocol,
		}
		if err := s.setMethod(protocol.Name, meth); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Register(receiver interface{}) error {
	typ := reflect.TypeOf(receiver)
	rcvr := reflect.ValueOf(receiver)

	if err := s.register(rcvr, typ); err != nil {
		return err
	}
	if err := s.register(rcvr, reflect.PtrTo(typ)); err != nil {
		return err
	}
	return nil
}

func (s *Service) WritePacket(msg []byte) error {
	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

	sz := len(msg)
	if sz > MSG_MAX_LEN {
		return fmt.Errorf("sproto: message size(%d) should be less than %d", sz, MSG_MAX_LEN)
	}
	binary.BigEndian.PutUint16(s.wrbuf[:2], uint16(sz))
	copy(s.wrbuf[2:], msg)
	_, err := s.rw.Write(s.wrbuf[:sz+2])
	return err
}

func (s *Service) readPacket() (buf []byte, err error) {
	s.readMutex.Lock()
	defer s.readMutex.Unlock()

	var sz uint16
	if err = binary.Read(s.rw, binary.BigEndian, &sz); err != nil {
		return
	}

	var to uint16 = 0
	buf = s.rdbuf[:sz]
	for to < sz {
		var n int
		n, err = s.rw.Read(buf[to:])
		if err != nil {
			return
		}
		to += uint16(n)
	}
	return
}

// dispatch one packet
func (s *Service) DispatchOnce() error {
	data, err := s.readPacket()
	if err != nil {
		return err
	}

	mode, name, session, sp, err := s.rpc.Dispatch(data)
	if err != nil {
		return err
	}

	if mode == RpcRequestMode {
		method := s.getMethod(name)
		if method == nil {
			if err = s.onUnknown(mode, name, session, sp); err != nil {
				return err
			}
		}
		resp := method.call(sp)
		if method.protocol.HasResponse() {
			data, err := s.rpc.ResponseEncode(name, session, resp)
			if err != nil {
				return err
			}
			return s.WritePacket(data)
		}
	} else {
		call := s.grabSession(session)
		if call == nil {
			if err = s.onUnknown(mode, name, session, sp); err != nil {
				return err
			}
		}
		call.Resp = sp
		call.done()
	}
	return nil
}

// dispatch until error
func (s *Service) Dispatch() error {
	for {
		if err := s.DispatchOnce(); err != nil {
			return err
		}
	}
	return nil
}

// unblock call a service which has a reply
func (s *Service) Go(name string, req interface{}, done chan *Call) (call *Call, err error) {
	protocol := s.rpc.GetProtocolByName(name)
	if protocol == nil {
		err = fmt.Errorf("sproto: call unknown service: %s", name)
		return
	}

	if !protocol.HasResponse() {
		err = fmt.Errorf("sproto: can\\'t call service %s", name)
		return
	}

	session := s.nextSession()
	var data []byte
	if data, err = s.rpc.RequestEncode(name, session, req); err != nil {
		return
	}

	if done == nil {
		done = make(chan *Call, 1)
	} else {
		if cap(done) == 0 {
			err = fmt.Errorf("sproto: call %s with unbuffered done channel", name)
			return
		}
	}
	call = &Call{
		protocol: protocol,
		Done:     done,
	}
	s.setSession(session, call)
	s.WritePacket(data)
	return
}

// block call a service which has a reply
func (s *Service) Call(name string, req interface{}) (interface{}, error) {
	call, err := s.Go(name, req, nil)
	if err != nil {
		return nil, err
	}
	call = <-call.Done
	return call.Resp, call.Err
}

// encode notify packet
func (s *Service) Encode(name string, req interface{}) ([]byte, error) {
	return s.rpc.RequestEncode(name, 0, req)
}

// invoke a service which has not a reply
func (s *Service) Invoke(name string, req interface{}) error {
	data, err := s.rpc.RequestEncode(name, 0, req)
	if err != nil {
		return err
	}
	return s.WritePacket(data)
}

func (s *Service) SetOnUnknownPacket(onUnknown OnUnknownPacket) {
	s.onUnknown = onUnknown
}

func NewService(rw io.ReadWriter, protocols []*Protocol) (*Service, error) {
	rpc, err := NewRpc(protocols)
	if err != nil {
		return nil, err
	}
	return &Service{
		rpc:       rpc,
		rw:        rw,
		rdbuf:     make([]byte, MSG_MAX_LEN),
		wrbuf:     make([]byte, MSG_MAX_LEN+2),
		methods:   make(map[string]*Method),
		sessions:  make(map[int32]*Call),
		onUnknown: defaultOnUnknownPacket,
	}, nil
}
