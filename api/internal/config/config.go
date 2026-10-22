package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Config is intentionally environment based; no working-directory-dependent config file.
type Config struct {
	Addr       string
	DataDir    string
	LocalRoots []string
	OAuthURL   string
}

func Load() (Config, error) {
	addr := os.Getenv("CLOUD_API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return Config{}, fmt.Errorf("invalid CLOUD_API_ADDR: %w", err)
	}
	data := os.Getenv("CLOUD_DATA_DIR")
	host, _, _ := net.SplitHostPort(addr)
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return Config{}, fmt.Errorf("mount API currently requires a loopback CLOUD_API_ADDR until login authentication is implemented")
		}
	}
	if data == "" {
		data = "data"
	}
	roots := filepath.SplitList(os.Getenv("CLOUD_LOCAL_ROOTS"))
	if len(roots) == 0 {
		root := filepath.Join(data, "mounts")
		if err := os.MkdirAll(root, 0700); err != nil {
			return Config{}, err
		}
		roots = []string{root}
	}
	broker := strings.TrimRight(os.Getenv("CLOUD_OAUTH_URL"), "/")
	if broker != "" {
		u, err := url.Parse(broker)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return Config{}, fmt.Errorf("CLOUD_OAUTH_URL must be an HTTPS service URL")
		}
	}
	return Config{Addr: addr, DataDir: data, LocalRoots: roots, OAuthURL: broker}, nil
}
