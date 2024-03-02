package h2mux

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"golang.org/x/net/http2"
)

type ServerSession struct {
	conn            net.Conn
	c               *ServerConfig
	streams         int32
	queue           chan *ServerStream
	done            chan struct{}
	server          *http2.Server
	onceClose       sync.Once
	currentStreamId uint32
}

func newServerSession(conn net.Conn, c *ServerConfig) (*ServerSession, error) {
	session := &ServerSession{
		conn:  conn,
		c:     c,
		queue: make(chan *ServerStream, 64),
		done:  make(chan struct{}),
		server: &http2.Server{
			MaxReadFrameSize: uint32(c.MaxReadFrameSize),
			IdleTimeout:      c.IdleTimeout,
		},
	}
	session.asyncStart()
	return session, nil
}

func (s *ServerSession) asyncStart() {
	go func() {
		s.startHandleSession()
	}()
}

func (s *ServerSession) startHandleSession() {
	s.server.ServeConn(s.conn, &http2.ServeConnOpts{
		Handler: http.HandlerFunc(s.onClientRecv),
	})
	_ = s.Close()
}

func (s *ServerSession) onClientRecv(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	atomic.AddInt32(&s.streams, 1)
	defer atomic.AddInt32(&s.streams, -1)
	id := atomic.AddUint32(&s.currentStreamId, 1)
	w.WriteHeader(http.StatusOK)
	conn := newServerStream(s, id, w, r)
	s.queue <- conn
	select {
	case <-conn.done:
	case <-s.done:
		_ = conn.Close()
	}
}

func (s *ServerSession) Accept() (io.ReadWriteCloser, error) {
	return s.AcceptStream()
}

func (s *ServerSession) AcceptStream() (*ServerStream, error) {
	select {
	case ss := <-s.queue:
		return ss, nil
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	}
}

func (s *ServerSession) NumStreams() int {
	return int(atomic.LoadInt32(&s.streams))
}

func (s *ServerSession) Close() error {
	var err error
	s.onceClose.Do(func() {
		close(s.done)
		err = s.conn.Close()
	})
	return err
}

type ServerStream struct {
	lck       sync.Mutex
	s         *ServerSession
	w         http.ResponseWriter
	r         *http.Request
	flusher   http.Flusher
	done      chan struct{}
	isClosed  bool
	onceClose sync.Once
	id        uint32
}

func newServerStream(s *ServerSession, id uint32, w http.ResponseWriter, r *http.Request) *ServerStream {
	return &ServerStream{
		s:        s,
		w:        w,
		r:        r,
		isClosed: false,
		id:       id,
		flusher:  w.(http.Flusher),
		done:     make(chan struct{}),
	}
}

func (s *ServerStream) Read(b []byte) (int, error) {
	s.lck.Lock()
	defer s.lck.Unlock()
	if s.isClosed {
		return 0, fmt.Errorf("read on closed stream")
	}
	return s.r.Body.Read(b)
}

func (s *ServerStream) Write(b []byte) (int, error) {
	s.lck.Lock()
	defer s.lck.Unlock()
	if s.isClosed {
		return 0, fmt.Errorf("write on closed stream")
	}
	n, err := s.w.Write(b)
	if err != nil {
		return 0, err
	}
	s.flusher.Flush()
	return n, err

}

func (s *ServerStream) Close() error {
	s.lck.Lock()
	defer s.lck.Unlock()
	var err error
	s.onceClose.Do(func() {
		s.isClosed = true
		close(s.done)
		err = s.r.Body.Close()
	})
	return err
}

func (s *ServerStream) ID() uint32 {
	return s.id
}
