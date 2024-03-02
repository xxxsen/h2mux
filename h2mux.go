package h2mux

import (
	"fmt"
	"net"
	"time"
)

func checkClientConfig(c *ClientConfig) error {
	if c.MaxReadFrameSize < 16*1024 || c.MaxReadFrameSize > 16*1024*1024 {
		return fmt.Errorf("max read frame exceed")
	}
	if c.PingTimeout == 0 {
		return fmt.Errorf("invalid ping timeout")
	}
	return nil
}

func checkServerConfig(c *ServerConfig) error {
	if c.MaxReadFrameSize < 16*1024 || c.MaxReadFrameSize > 16*1024*1024 {
		return fmt.Errorf("max read frame exceed")
	}
	return nil
}

func Server(conn net.Conn, c *ServerConfig) (*ServerSession, error) {
	if err := checkServerConfig(c); err != nil {
		return nil, err
	}
	return newServerSession(conn, c)
}

func Client(conn net.Conn, c *ClientConfig) (*ClientSession, error) {
	if err := checkClientConfig(c); err != nil {
		return nil, err
	}
	return newClientSession(conn, c)
}

func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		MaxReadFrameSize: 16 * 1024,
	}
}

func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		MaxReadFrameSize: 16 * 1024,
		PingTimeout:      10 * time.Second,
	}
}
