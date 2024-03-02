package h2mux

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
)

type ClientSession struct {
	conn             net.Conn
	c                *ClientConfig
	tr               *http2.Transport
	cc               *http2.ClientConn
	onceClose        sync.Once
	currentSessionId uint32
}

func newClientSession(conn net.Conn, c *ClientConfig) (*ClientSession, error) {
	session := &ClientSession{
		tr: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
				return conn, nil
			},
			MaxReadFrameSize: uint32(c.MaxReadFrameSize),
			PingTimeout:      c.PingTimeout,
		},
		c:    c,
		conn: conn,
	}
	cc, err := session.tr.NewClientConn(conn)
	if err != nil {
		return nil, err
	}
	session.tr.ConnPool = newClientSessionConnPool(session, cc)
	session.cc = cc
	return session, nil
}

type clientSessionConnPool struct {
	sess *ClientSession
	cc   *http2.ClientConn
}

func newClientSessionConnPool(sess *ClientSession, cc *http2.ClientConn) *clientSessionConnPool {
	return &clientSessionConnPool{
		sess: sess,
		cc:   cc,
	}
}

func (c *clientSessionConnPool) GetClientConn(req *http.Request, addr string) (*http2.ClientConn, error) {
	return c.cc, nil
}

func (c *clientSessionConnPool) MarkDead(conn *http2.ClientConn) {
	c.sess.Close()
}

func (s *ClientSession) nextSessionId() uint32 {
	return atomic.AddUint32(&s.currentSessionId, 1)
}

func (s *ClientSession) Open() (io.ReadWriteCloser, error) {
	return s.OpenStream()
}

func (s *ClientSession) OpenStream() (*ClientStream, error) {
	if s.cc.State().Closed {
		return nil, fmt.Errorf("connection closed")
	}
	cs, err := newClientStream(s)
	if err != nil {
		return nil, err
	}
	return cs, nil
}

func (s *ClientSession) NumStreams() int {
	return s.cc.State().StreamsActive
}

func (s *ClientSession) Close() error {
	var err error
	s.onceClose.Do(func() {
		err = s.cc.Close()
	})
	return err
}

func newClientStream(s *ClientSession) (*ClientStream, error) {
	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodPut, "http://127.1", io.NopCloser(pr))
	if err != nil {
		return nil, err
	}

	cs := &ClientStream{s: s, writeCloser: pw, id: s.nextSessionId()}
	cs.waiter.Add(1)
	go func() {
		defer cs.waiter.Done()
		resp, err := s.tr.RoundTrip(req)
		if err != nil {
			cs.markBodyReady(nil, err)
			return
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			cs.markBodyReady(nil, fmt.Errorf("status code not ok, code:%d", resp.StatusCode))
			return
		}
		cs.markBodyReady(resp.Body, nil)
	}()
	return cs, nil
}

type ClientStream struct {
	s         *ClientSession
	id        uint32
	onceClose sync.Once
	//
	readCloser  io.ReadCloser
	writeCloser io.WriteCloser
	//
	initErr error
	waiter  sync.WaitGroup
}

func (c *ClientStream) markBodyReady(rc io.ReadCloser, err error) {
	c.readCloser = rc
	c.initErr = err
}

func (c *ClientStream) Read(b []byte) (int, error) {
	if c.readCloser == nil {
		c.waiter.Wait()
		if c.initErr != nil {
			return 0, c.initErr
		}
	}

	return c.readCloser.Read(b)
}

func (c *ClientStream) Write(b []byte) (int, error) {
	return c.writeCloser.Write(b)
}

func (c *ClientStream) Close() error {
	var err error
	c.onceClose.Do(func() {
		var werr, rerr error
		if c.writeCloser != nil {
			werr = c.writeCloser.Close()
		}
		//关闭rsp.Body会设置abortErr, 这个错误在req.Body读取并写入到远端后会进行判断, 如果存在这个错误, 则会触发一个stream cancel的错误
		//这个req.Body读取逻辑在roundTrip里面, 是个异步流程, 所以如果关闭req.Body后, 快速关闭rsp.Body 就可能会导致写入abortErr的这个操作先被执行而导致
		//最终触发stream cancel错误.
		//这个错误貌似没什么影响？
		//
		//补充延迟, 减少触发的几率
		time.Sleep(10 * time.Millisecond)
		if c.readCloser != nil {
			rerr = c.readCloser.Close()
		}
		if werr != nil || rerr != nil {
			err = fmt.Errorf("close client stream failed, rerr:%v, werr:%v", rerr, werr)
		}
	})
	return err
}

func (c *ClientStream) ID() uint32 {
	return c.id
}
