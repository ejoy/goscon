package scp

import (
	"bufio"
	"crypto/rc4"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"

	"github.com/ejoy/goscon/dh64"
	"github.com/golang/glog"
)

type cipherConnReader struct {
	sync.Mutex
	rd     io.Reader
	cipher *rc4.Cipher
	count  int // bytes read
}

type cipherConnWriter struct {
	sync.Mutex
	wr     io.Writer
	cipher *rc4.Cipher
	count  int // bytes writed
}

func genRC4Key(v1 leu64, v2 leu64, key []byte) {
	h := hmac(v1, v2)
	copy(key, h[:])
}

func (c *cipherConnReader) SetReader(rd io.Reader) {
	c.Lock()
	defer c.Unlock()
	c.rd = rd
}

func (c *cipherConnReader) GetBytesReceived() int {
	return c.count
}

func (c *cipherConnReader) Read(p []byte) (n int, err error) {
	c.Lock()
	defer c.Unlock()
	n, err = c.rd.Read(p)
	if err != nil {
		return
	}
	c.cipher.XORKeyStream(p[:n], p[:n])
	c.count += n
	return
}

func (c *cipherConnWriter) SetWriter(wr io.Writer) {
	c.Lock()
	defer c.Unlock()
	c.wr = wr
}

func (c *cipherConnWriter) GetBytesSent() int {
	return c.count
}

func (c *cipherConnWriter) Write(b []byte) (int, error) {
	c.Lock()
	defer c.Unlock()

	sz := len(b)
	buf := defaultBufferPool.Get(sz)
	defer defaultBufferPool.Put(buf)

	space := buf.Bytes()[:sz]
	c.cipher.XORKeyStream(space, b)
	c.count += sz
	_, err := c.wr.Write(space)
	return sz, err
}

func deepCopyCipherConnReader(in *cipherConnReader) *cipherConnReader {
	return &cipherConnReader{
		cipher: &(*in.cipher),
		count:  in.count,
	}
}

func deepCopyCipherConnWriter(out *cipherConnWriter) *cipherConnWriter {
	return &cipherConnWriter{
		cipher: &(*out.cipher),
		count:  out.count,
	}
}

func newCipherConnReader(secret leu64) *cipherConnReader {
	b := defaultBufferPool.Get(32)
	defer defaultBufferPool.Put(b)
	key := b.Bytes()[:32]

	genRC4Key(secret, toLeu64(0), key[0:8])
	genRC4Key(secret, toLeu64(1), key[8:16])
	genRC4Key(secret, toLeu64(2), key[16:24])
	genRC4Key(secret, toLeu64(3), key[24:32])

	c, _ := rc4.NewCipher(key)
	return &cipherConnReader{
		cipher: c,
	}
}

func newCipherConnWriter(secret leu64) *cipherConnWriter {
	b := defaultBufferPool.Get(32)
	defer defaultBufferPool.Put(b)
	key := b.Bytes()[:32]

	genRC4Key(secret, toLeu64(0), key[0:8])
	genRC4Key(secret, toLeu64(1), key[8:16])
	genRC4Key(secret, toLeu64(2), key[16:24])
	genRC4Key(secret, toLeu64(3), key[24:32])

	c, _ := rc4.NewCipher(key)
	return &cipherConnWriter{
		cipher: c,
	}
}

// Conn .
type Conn struct {
	// constant
	conn   net.Conn
	config *Config

	connMutex  sync.Mutex
	connErr    error
	handshaked bool // handshake finish flag
	frozen     bool // if conn is frozen, read/write will failed immediately

	// half conn
	in  *cipherConnReader
	out *cipherConnWriter

	// reuse
	id         int
	handshakes int
	secret     leu64

	reuseBuffer *loopBuffer

	reused bool // reused conn
}

func (c *Conn) initNewConn(id int, secret leu64) {
	c.id = id
	c.secret = secret
	c.reuseBuffer = defaultLoopBufferPool.Get()

	c.in = newCipherConnReader(c.secret)
	c.out = newCipherConnWriter(c.secret)
	c.in.SetReader(c.conn)
	c.out.SetWriter(io.MultiWriter(c.reuseBuffer, c.conn))

	c.reused = false
}

// copy status to new conn
func (c *Conn) spawn(new *Conn) bool {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()
	c.freeze()

	if c.reuseBuffer == nil { // c is closed
		return false
	}

	// not reading
	c.in.Lock()
	c.in.Unlock()

	// not writing
	c.out.Lock()
	c.out.Unlock()

	new.id = c.id
	new.secret = c.secret

	reuseBuffer := defaultLoopBufferPool.Get()
	c.reuseBuffer.CopyTo(reuseBuffer)
	new.reuseBuffer = reuseBuffer
	new.in = deepCopyCipherConnReader(c.in)
	new.out = deepCopyCipherConnWriter(c.out)
	new.in.SetReader(new.conn)
	new.out.SetWriter(io.MultiWriter(new.reuseBuffer, new.conn))

	new.reused = true
	return true
}

func (c *Conn) writeRecord(msg handshakeMessage) error {
	data := msg.marshal()
	sz := uint16(len(data))

	w := bufio.NewWriter(c.conn)
	err := binary.Write(w, binary.BigEndian, sz)
	if err != nil {
		return err
	}

	if _, err := w.Write(data); err != nil {
		return err
	}

	return w.Flush()
}

func (c *Conn) readRecord(msg handshakeMessage) error {
	var sz uint16
	if err := binary.Read(c.conn, binary.BigEndian, &sz); err != nil {
		return err
	}

	buf := defaultBufferPool.Get(int(sz))
	defer defaultBufferPool.Put(buf)

	b := buf.Bytes()[:sz]
	if _, err := io.ReadFull(c.conn, b); err != nil {
		return err
	}

	if err := msg.unmarshal(b); err != nil {
		return err
	}
	return nil
}

func (c *Conn) clientReuseHandshake() error {
	rq := &reuseConnReq{
		id:         c.id,
		handshakes: c.handshakes,
		received:   uint32(c.in.GetBytesReceived()),
	}

	// fill checksum
	rq.setSum(c.secret)
	if err := c.writeRecord(rq); err != nil {
		return err
	}

	var rp reuseConnResp
	if err := c.readRecord(&rp); err != nil {
		return err
	}

	if err := newError(rp.code); err != nil {
		return err
	}

	diff := c.out.GetBytesSent() - int(rp.received)
	if diff < 0 || diff > c.reuseBuffer.Len() {
		// TODO: warning
		return ErrNotAcceptable
	}

	if diff > 0 {
		lastBytes, err := c.reuseBuffer.ReadLastBytes(diff)
		if err != nil {
			return err
		}

		if _, err = c.conn.Write(lastBytes); err != nil {
			return err
		}

		if glog.V(1) {
			glog.Infof("client retrans packets: addr=%s sz=%d", c.conn.LocalAddr(), diff)
		}
	}

	return nil
}

func (c *Conn) clientNewHandshake() error {
	priKey := dh64.PrivateKey()
	pubKey := dh64.PublicKey(priKey)

	nq := &newConnReq{
		id:           0,
		key:          toLeu64(pubKey),
		targetServer: c.config.TargetServer,
	}

	if err := c.writeRecord(nq); err != nil {
		return err
	}

	var np newConnResp
	if err := c.readRecord(&np); err != nil {
		return err
	}

	if np.id == 0 {
		panic("np.id == 0")
	}

	secret := dh64.Secret(priKey, np.key.Uint64())
	c.initNewConn(np.id, toLeu64(secret))
	return nil
}

func (c *Conn) clientHandshake() error {
	if c.id != 0 {
		return c.clientReuseHandshake()
	}
	return c.clientNewHandshake()
}

func (c *Conn) serverReuseHandshake(rq *reuseConnReq) error {
	diff := 0
	rp := &reuseConnResp{
		received: 0,
		code:     SCPStatusOK,
	}

OuterLoop:
	for {
		oldConn := c.config.ScpServer.QueryByID(rq.id)
		if oldConn == nil {
			rp.code = SCPStatusIDNotFound
			break OuterLoop
		}

		if !rq.verifySum(oldConn.secret) {
			rp.code = SCPStatusUnauthorized
			break OuterLoop
		}

		if oldConn.handshakes >= rq.handshakes {
			rp.code = SCPStatusExpired
			break OuterLoop
		}
		c.handshakes = rq.handshakes

		// all check pass, spawn new conn
		if !oldConn.spawn(c) {
			rp.code = SCPStatusIDNotFound
			break OuterLoop
		}

		diff = c.out.GetBytesSent() - int(rq.received)
		if diff < 0 || diff > c.reuseBuffer.Len() {
			rp.code = SCPStatusNotAcceptable
			break OuterLoop
		}

		rp.received = uint32(c.in.GetBytesReceived())
		break OuterLoop
	}

	if err := c.writeRecord(rp); err != nil {
		return err
	}

	if err := newError(rp.code); err != nil {
		return err
	}

	if diff > 0 {
		lastBytes, err := c.reuseBuffer.ReadLastBytes(int(diff))
		if err != nil {
			return err
		}

		if _, err = c.conn.Write(lastBytes); err != nil {
			return err
		}

		if glog.V(1) {
			glog.Infof("server retrans packets: addr=%s sz=%d", c.conn.RemoteAddr(), diff)
		}
	}
	return nil
}

func (c *Conn) serverNewHandshake(nq *newConnReq) error {
	priKey := dh64.PrivateKey()
	pubKey := dh64.PublicKey(priKey)

	id := c.config.ScpServer.AcquireID()
	np := &newConnResp{
		id:  id,
		key: toLeu64(pubKey),
	}

	if err := c.writeRecord(np); err != nil {
		c.config.ScpServer.ReleaseID(id)
		return err
	}

	secret := dh64.Secret(priKey, nq.key.Uint64())
	c.initNewConn(id, toLeu64(secret))

	// set preferred target
	c.config.TargetServer = nq.targetServer
	return nil
}

func (c *Conn) serverHandshake() error {
	var sq serverReq
	if err := c.readRecord(&sq); err != nil {
		return err
	}

	switch q := sq.msg.(type) {
	case *newConnReq:
		return c.serverNewHandshake(q)
	case *reuseConnReq:
		return c.serverReuseHandshake(q)
	}
	return nil
}

func (c *Conn) setConnErr(err error) {
	if c.connErr == nil {
		c.connErr = err
	}
}

// Handshake .
func (c *Conn) Handshake() error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()
	if c.handshaked {
		return c.connErr
	}

	var err error
	if c.config.ScpServer == nil {
		err = c.clientHandshake()
	} else {
		err = c.serverHandshake()
	}

	c.setConnErr(err)
	c.handshaked = true
	return err
}

// Write writes data to the connection and cache in reuseBuffer
// even failed to write to the connection, the data should still be cached
func (c *Conn) Write(b []byte) (int, error) {
	if err := c.Handshake(); err != nil {
		return 0, err
	}
	return c.out.Write(b)
}

// Read can be made to time out and return a net.Error with Timeout() == true
// after a fixed time limit; see SetDeadline and SetReadDeadline.
func (c *Conn) Read(b []byte) (int, error) {
	if err := c.Handshake(); err != nil {
		return 0, err
	}

	if len(b) == 0 {
		return 0, nil
	}
	return c.in.Read(b)
}

// LocalAddr returns the local network address.
func (c *Conn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// SetDeadline sets the read and write deadlines associated with the connection.
// A zero value for t means Read and Write will not time out.
// After a Write has timed out, the TLS state is corrupt and all future writes will return the same error.
func (c *Conn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline on the underlying connection.
// A zero value for t means Read will not time out.
func (c *Conn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline on the underlying connection.
// A zero value for t means Write will not time out.
// After a Write has timed out, the TLS state is corrupt and all future writes will return the same error.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func (c *Conn) freeze() {
	if c.frozen {
		return
	}
	c.frozen = true

	err := c.conn.Close()
	if err == nil {
		err = io.ErrClosedPipe
	}
	c.setConnErr(err)
}

// Freeze make conn frozen, and wait for resue
func (c *Conn) Freeze() {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()
	c.freeze()
}

// Close closes raw conn and releases all resources. After close, c can't be reused.
func (c *Conn) Close() error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()
	c.freeze()

	if c.reuseBuffer != nil {
		defaultLoopBufferPool.Put(c.reuseBuffer)
		c.reuseBuffer = nil
	}
	return nil
}

// ID .
func (c *Conn) ID() int {
	return c.id
}

// IsReused .
func (c *Conn) IsReused() bool {
	return c.reused
}

// TargetServer .
func (c *Conn) TargetServer() string {
	return c.config.TargetServer
}
