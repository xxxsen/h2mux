package h2mux

import "time"

type ClientConfig struct {
	MaxReadFrameSize int
	PingTimeout      time.Duration
}

type ServerConfig struct {
	MaxReadFrameSize int
	IdleTimeout      time.Duration
}
