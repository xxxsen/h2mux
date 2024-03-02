package h2mux

import (
	"encoding/hex"
	"io"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var testLink = "/tmp/echo.sock"

type dumpConn struct {
	name string
	net.Conn
}

func newDumpConn(name string, c net.Conn) net.Conn {
	return &dumpConn{name: name, Conn: c}
}

func (r *dumpConn) Read(b []byte) (int, error) {
	n, err := r.Conn.Read(b)
	if err == nil {
		log.Printf("name:%s, read data:%s", r.name, hex.EncodeToString(b[:n]))
	}
	return n, err
}

func (r *dumpConn) Write(b []byte) (int, error) {
	n, err := r.Conn.Write(b)
	if err == nil {
		log.Printf("name:%s, write data:%s", r.name, hex.EncodeToString(b))
	}
	return n, err
}

func startServerStream(t *testing.T, ss *ServerStream) {
	n, err := io.Copy(ss, ss)
	assert.NoError(t, err)
	log.Printf("copy stream finish, count:%d", n)
}

func startServerSession(t *testing.T, conn net.Conn) {
	conn = newDumpConn("server", conn)
	session, err := Server(conn, DefaultServerConfig())
	assert.NoError(t, err)
	defer session.Close()
	for {
		ss, err := session.AcceptStream()
		if err != nil {
			log.Printf("accept stream failed, err:%v", err)
			break
		}
		log.Printf("recv stream from client, id:%d", ss.ID())
		go func() {
			startServerStream(t, ss)
		}()
	}
}

func startServer(t *testing.T) {
	ls, err := net.Listen("unix", testLink)
	assert.NoError(t, err)
	for {
		conn, err := ls.Accept()
		assert.NoError(t, err)
		go func() {
			startServerSession(t, conn)
		}()
	}
}

func TestMux(t *testing.T) {
	os.Remove(testLink)
	go startServer(t)
	time.Sleep(1 * time.Second)
	testClient(t)
	time.Sleep(1 * time.Second)
}

func testClient(t *testing.T) {
	conn, err := net.Dial("unix", testLink)
	assert.NoError(t, err)
	defer conn.Close()
	conn = newDumpConn("client", conn)
	sess, err := Client(conn, DefaultClientConfig())
	assert.NoError(t, err)
	defer sess.Close()
	startClient(t, sess)
	startClient(t, sess)
}

func startClient(t *testing.T, sess *ClientSession) {
	ss, err := sess.OpenStream()
	assert.NoError(t, err)
	log.Printf("start client, sessionid:%d", ss.ID())
	defer ss.Close()
	buf := []byte("hello world, test client")
	_, err = ss.Write(buf)
	assert.NoError(t, err)
	rev := make([]byte, len(buf))
	n, err := io.ReadAtLeast(ss, rev, len(rev))
	assert.NoError(t, err)
	assert.Equal(t, len(buf), n)
	assert.Equal(t, buf, rev[:n])
	log.Printf("recv server echo:%s, begin close conn", string(rev[:n]))
}
