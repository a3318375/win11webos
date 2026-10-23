package mount

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type cacheEntry struct {
	items   []Entry
	fetched time.Time
	expires time.Time
	bytes   int
}
type Listing struct {
	Items     []Entry   `json:"items"`
	CacheHit  bool      `json:"cacheHit"`
	FetchedAt time.Time `json:"fetchedAt"`
	Total     int       `json:"total"`
}
type Service struct {
	disks            *DiskMonitor
	store            *Store
	roots            []string
	client           *http.Client
	broker           string
	mu               sync.Mutex
	cache            map[string]cacheEntry
	cacheBytes       int
	group            singleflight.Group
	authMu           sync.Mutex
	sessions         map[string]*authSession
	accountLocks     sync.Map
	knownDirectories map[string]map[string]bool
	transfers        *Transfers
}

func ID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func New(store *Store, roots []string, broker string) (*Service, error) {
	s := &Service{store: store, cache: map[string]cacheEntry{}, client: &http.Client{Timeout: 25 * time.Second}, broker: strings.TrimRight(broker, "/"), sessions: map[string]*authSession{}, knownDirectories: map[string]map[string]bool{}}
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		real, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(real)
		if err != nil || !info.IsDir() {
			return nil, errors.New("CLOUD_LOCAL_ROOTS 必须是已存在目录")
		}
		s.roots = append(s.roots, real)
	}
	if err := s.initTransfers(); err != nil {
		return nil, err
	}
	s.disks = newDiskMonitor(s.roots)
	return s, nil
}
func (s *Service) Close() error {
	if s.disks != nil {
		s.disks.cancel()
	}
	if s.transfers != nil {
		s.transfers.close()
	}
	return s.store.Close()
}
func (s *Service) Accounts(ctx context.Context) ([]Account, error) { return s.store.List(ctx) }
func (s *Service) validate(ctx context.Context, in Input) (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 50 {
		return in, errors.New("账号名称需为 1～50 个字符")
	}
	if in.Config.CacheMinutes < 0 || in.Config.CacheMinutes > 10080 {
		return in, errors.New("缓存时间应为 0～10080 分钟")
	}
	if in.AuthSession != "" {
		creds, err := s.sessionCredentials(in.AuthSession, in.Provider)
		if err != nil {
			return in, err
		}
		in.Credentials = creds
	}
	switch in.Provider {
	case "nas":
		root, err := s.validateLocal(in.Config.Root)
		if err != nil {
			return in, err
		}
		in.Config.Root = root
		in.Credentials = Credentials{}
	case "115", "baidu", "quark":
		if in.Config.Root == "" {
			if in.Provider == "baidu" {
				in.Config.Root = "/"
			} else {
				in.Config.Root = "0"
			}
		}
		if in.Provider == "quark" {
			if strings.TrimSpace(in.Credentials.Cookie) == "" {
				return in, errors.New("请扫码授权或填写 Cookie")
			}
			in.Config.DownloadMode = "proxy"
		} else if in.Credentials.Access == "" && in.Credentials.Refresh == "" {
			return in, errors.New("请自动获取 Token 或填写凭据")
		}
		if in.Provider == "baidu" && !strings.HasPrefix(in.Config.Root, "/") {
			return in, errors.New("百度根目录应以 / 开头")
		}
		if in.Config.DownloadMode == "" {
			in.Config.DownloadMode = "proxy"
		}
		if in.Config.DownloadMode != "proxy" && in.Config.DownloadMode != "redirect" {
			return in, errors.New("下载模式无效")
		}
	default:
		return in, errors.New("暂不支持此存储类型")
	}
	return in, nil
}
func (s *Service) Test(ctx context.Context, in Input) (Input, int, error) {
	in, err := s.validate(ctx, in)
	if err != nil {
		return in, 0, err
	}
	items, creds, err := s.read(ctx, Account{Provider: in.Provider, Config: in.Config}, in.Credentials)
	in.Credentials = creds
	return in, len(items), err
}
func (s *Service) Create(ctx context.Context, in Input) (Account, error) {
	normalized, _, err := s.Test(ctx, in)
	if err != nil {
		return Account{}, err
	}
	a := Account{ID: ID(), Name: normalized.Name, Provider: normalized.Provider, Config: normalized.Config, CreatedAt: time.Now()}
	if err = s.store.Put(ctx, a, normalized.Credentials); err != nil {
		return Account{}, err
	}
	if in.AuthSession != "" {
		s.authMu.Lock()
		delete(s.sessions, in.AuthSession)
		s.authMu.Unlock()
	}
	return a, nil
}
func (s *Service) Remove(ctx context.Context, id string) error {
	lock, _ := s.accountLocks.LoadOrStore(id, &sync.Mutex{})
	m := lock.(*sync.Mutex)
	m.Lock()
	defer m.Unlock()
	if err := s.store.Remove(ctx, id); err != nil {
		return err
	}
	s.invalidate(id)
	return nil
}
func (s *Service) invalidate(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.knownDirectories, id)
	for key := range s.cache {
		if strings.HasPrefix(key, id+":") {
			s.cacheBytes -= s.cache[key].bytes
			delete(s.cache, key)
		}
	}
}
func (s *Service) List(ctx context.Context, id, parent string, refresh bool) (Listing, error) {
	if parent == "root" {
		parent = ""
	}
	a, _, err := s.store.Get(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	key := id + ":" + parent
	if a.Provider != "nas" && parent != "" && parent != a.Config.Root {
		s.mu.Lock()
		allowed := s.knownDirectories[id][parent]
		s.mu.Unlock()
		if !allowed {
			return Listing{}, errors.New("请从挂载根目录逐级进入文件夹")
		}
	}
	if !refresh && a.Config.CacheMinutes > 0 {
		s.mu.Lock()
		cached, ok := s.cache[key]
		s.mu.Unlock()
		if ok && time.Now().Before(cached.expires) {
			return Listing{Items: cached.items, CacheHit: true, FetchedAt: cached.fetched, Total: len(cached.items)}, nil
		}
	}
	flight := key
	if refresh {
		flight += "|refresh"
	}
	ch := s.group.DoChan(flight, func() (any, error) {
		// One read per account also serializes rotating refresh tokens.
		lock, _ := s.accountLocks.LoadOrStore(id, &sync.Mutex{})
		m := lock.(*sync.Mutex)
		m.Lock()
		defer m.Unlock()
		call, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
		defer cancel()
		account, creds, err := s.store.Get(call, id)
		if err != nil {
			return nil, err
		}
		if !refresh && account.Config.CacheMinutes > 0 {
			s.mu.Lock()
			cached, ok := s.cache[key]
			s.mu.Unlock()
			if ok && time.Now().Before(cached.expires) {
				return Listing{Items: cached.items, CacheHit: true, FetchedAt: cached.fetched, Total: len(cached.items)}, nil
			}
		}
		config := account.Config
		var items []Entry
		updated := creds
		if account.Provider == "nas" {
			if _, err = s.validateLocal(config.Root); err != nil {
				return nil, err
			}
			items, err = localList(call, config.Root, parent)
		} else {
			if parent != "" {
				account.Config.Root = parent
			}
			items, updated, err = s.read(call, account, creds)
		}
		if err != nil {
			return nil, err
		}
		if updated != creds {
			if err = s.store.UpdateCredentials(call, id, updated); err != nil {
				return nil, errors.New("更新授权凭据失败")
			}
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Directory != items[j].Directory {
				return items[i].Directory
			}
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		})
		now := time.Now()
		s.mu.Lock()
		if s.knownDirectories[id] == nil {
			s.knownDirectories[id] = map[string]bool{}
		}
		for _, item := range items {
			if item.Directory {
				s.knownDirectories[id][item.ID] = true
			}
		}
		s.mu.Unlock()
		if config.CacheMinutes > 0 && len(items) <= 20000 {
			s.mu.Lock()
			cost := 0
			for _, item := range items {
				cost += 160 + len(item.ID) + len(item.Name)
			}
			if old, ok := s.cache[key]; ok {
				s.cacheBytes -= old.bytes
				delete(s.cache, key)
			}
			for len(s.cache) > 0 && (len(s.cache) >= 128 || s.cacheBytes+cost > 32*1024*1024) {
				var oldest string
				var oldestTime time.Time
				for k, v := range s.cache {
					if oldest == "" || v.fetched.Before(oldestTime) {
						oldest = k
						oldestTime = v.fetched
					}
				}
				s.cacheBytes -= s.cache[oldest].bytes
				delete(s.cache, oldest)
			}
			if cost <= 32*1024*1024 {
				s.cache[key] = cacheEntry{items: items, fetched: now, expires: now.Add(time.Duration(config.CacheMinutes) * time.Minute), bytes: cost}
				s.cacheBytes += cost
			}
			s.mu.Unlock()
		}
		return Listing{Items: items, FetchedAt: now, Total: len(items)}, nil
	})
	select {
	case <-ctx.Done():
		return Listing{}, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			return Listing{}, result.Err
		}
		return result.Val.(Listing), nil
	}
}
func (s *Service) read(ctx context.Context, a Account, c Credentials) ([]Entry, Credentials, error) {
	if a.Provider == "nas" {
		items, err := localList(ctx, a.Config.Root, "")
		return items, c, err
	}
	return s.remoteList(ctx, a, c)
}
