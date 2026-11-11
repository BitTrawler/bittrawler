package metainforequester

import "time"

type Config struct {
	// RequestTimeout bounds the whole exchange: connect, handshakes and
	// metadata transfer.
	RequestTimeout time.Duration
	// DialTimeout bounds only the TCP connect.
	DialTimeout time.Duration
	// Per-node request rate limit, in requests per second.
	PerNodeRate  float64
	PerNodeBurst int
	LimiterSize  int
	LimiterTTL   time.Duration
}

func NewDefaultConfig() Config {
	return Config{
		RequestTimeout: 6 * time.Second,
		DialTimeout:    3 * time.Second,
		PerNodeRate:    2,
		PerNodeBurst:   4,
		LimiterSize:    1000,
		LimiterTTL:     20 * time.Second,
	}
}
