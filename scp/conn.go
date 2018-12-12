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
	buf    []byte
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
	if sz > cap(c.buf) {
		c.buf = make([]byte, sz*2)
	}

	buf := c.buf[:sz]
	c.cipher.XORKeyStream(buf, b)
	c.count += sz
	_, err := c.wr.Write(buf)
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
	key := make([]byte, 32)
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
	key := make([]byte, 32)
	genRC4Key(secret, toLeu64(0), key[0:8])
	genRC4Key(secret, toLeu64(1), key[8:16])
	genRC4Key(secret, toLeu64(2), key[16:24])
	genRC4Key(secret, toLeu64(3), key[24:32])

	c, _ := rc4.NewCipher(key)
	return &cipherConnWriter{
		cipher: c,
		buf:    make([]byte, NetBufferSize),
	}
}

type Conn struct {
	// constant
	conn   net.Conn
	config *Config

	handshakeMutex    sync.Mutex
	handshakeErr      error
	handshakeComplete bool

	// half conn
	in  *cipherConnReader
	out *cipherConnWriter

	// reuse
	id         int
	handshakes int
	secret     leu64

	sentCache *loopBuffer

	reused bool // reused conn
}

func (c *Conn) initNewConn(id int, secret leu64) {
	c.id = id
	c.secret = secret
	c.sentCache = newLoopBuffer(SentCacheSize)

	c.in = newCipherConnReader(c.secret)
	c.out = newCipherConnWriter(c.secret)
	c.in.SetReader(c.conn)
	c.out.SetWriter(io.MultiWriter(c.sentCache, c.conn))

	c.reused = false
}

func (c *Conn) initReuseConn(oldConn *Conn, handshakes int) {
	c.id = oldConn.id
	c.handshakes = handshakes
	c.secret = oldConn.secret

	c.sentCache = deepCopyLoopBuffer(oldConn.sentCache)
	c.in = deepCopyCipherConnReader(oldConn.in)
	c.out = deepCopyCipherConnWriter(oldConn.out)
	c.in.SetReader(c.conn)
	c.out.SetWriter(io.MultiWriter(c.sentCache, c.conn))

	c.reused = true
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

	buf := make([]byte, sz)
	sum := 0
	for sum < int(sz) {
		n, err := c.conn.Read(buf[sum:])
		if err != nil {
			return err
		}
		sum += n
	}

	if err := msg.unmarshal(buf); err != nil {
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

	if err := newError(rp.code, ""); err != nil {
		return err
	}

	diff := c.out.GetBytesSent() - int(rp.received)
	if diff < 0 || diff > c.sentCache.Len() {
		return fmt.Errorf("406 Not Acceptable")
	}

	if diff > 0 {
		lastBytes, err := c.sentCache.ReadLastBytes(diff)
		if err != nil {
			return err
		}

		if _, err = c.conn.Write(lastBytes); err != nil {
			return err
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
	} else {
		return c.clientNewHandshake()
	}
}

func (c *Conn) serverReuseHandshake(rq *reuseConnReq) error {
	diff := 0
	rp := &reuseConnResp{
		received: 0,
		code:     SCPStatusOK,
	}
	extra := fmt.Sprintf("conn id[%d]", rq.id)
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

		// all check pass, close old
		oldConn = c.config.ScpServer.CloseByID(rq.id)

		// double check
		if oldConn == nil {
			rp.code = SCPStatusIDNotFound
			break OuterLoop
		}

		diff = oldConn.out.GetBytesSent() - int(rq.received)
		if diff < 0 || diff > oldConn.sentCache.Len() {
			rp.code = SCPStatusNotAcceptable
			break OuterLoop
		}
		c.initReuseConn(oldConn, rq.handshakes)
		rp.received = uint32(c.in.GetBytesReceived())
		break OuterLoop
	}

	if err := c.writeRecord(rp); err != nil {
		return err
	}

	if err := newError(rp.code, extra); err != nil {
		return err
	}

	if diff > 0 {
		lastBytes, err := c.sentCache.ReadLastBytes(int(diff))
		if err != nil {
			return err
		}

		if _, err = c.conn.Write(lastBytes); err != nil {
			return err
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

func (c *Conn) Handshake() error {
	if c.handshakeComplete {
		return nil
	}

	c.handshakeMutex.Lock()
	defer c.handshakeMutex.Unlock()

	if err := c.handshakeErr; err != nil {
		return err
	}
	if c.handshakeComplete {
		return nil
	}

	if c.config.ScpServer == nil {
		c.handshakeErr = c.clientHandshake()
	} else {
		c.handshakeErr = c.serverHandshake()
	}

	if c.handshakeErr != nil {
		return c.handshakeErr
	}

	c.handshakeComplete = true
	return nil
}

// Write writes data to the connection and cache in sentCache
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

func (c *Conn) Close() error {
	return c.conn.Close()
}

func (c *Conn) RawConn() net.Conn {
	return c.conn
}

func (c *Conn) ID() int {
	return c.id
}

func (c *Conn) IsReused() bool {
	return c.reused
}

func (c *Conn) TargetServer() string {
	return c.config.TargetServer
}
