package config

import (
	"fmt"
	"net"
	"os"
)

// Config is intentionally environment based; no working-directory-dependent config file.
type Config struct {
	Addr string
}

func Load() (Config, error) {
	addr := os.Getenv("CLOUD_API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return Config{}, fmt.Errorf("invalid CLOUD_API_ADDR: %w", err)
	}
	return Config{Addr: addr}, nil
}
