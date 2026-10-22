package mount

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Credentials struct {
	Access  string `json:"accessToken"`
	Refresh string `json:"refreshToken"`
	Cookie  string `json:"cookie"`
}
type Config struct {
	Root         string `json:"root"`
	CacheMinutes int    `json:"cacheMinutes"`
	DownloadMode string `json:"downloadMode"`
}
type Account struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Config    Config    `json:"config"`
	CreatedAt time.Time `json:"createdAt"`
}
type Input struct {
	Name        string      `json:"name"`
	Provider    string      `json:"provider"`
	Config      Config      `json:"config"`
	Credentials Credentials `json:"credentials"`
	AuthSession string      `json:"authSession,omitempty"`
}
type Store struct {
	db   *sql.DB
	aead cipher.AEAD
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "credentials.key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		if _, dbErr := os.Stat(filepath.Join(dir, "cloud.db")); dbErr == nil {
			return nil, errors.New("credentials.key 缺失，请恢复原密钥后启动，避免已有账号无法解密")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		_, err = f.Write(key)
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	} else if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("invalid credential key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "cloud.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY,name TEXT NOT NULL COLLATE NOCASE UNIQUE,provider TEXT NOT NULL,config TEXT NOT NULL,secret BLOB NOT NULL,created_at TEXT NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, aead: aead}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Put(ctx context.Context, a Account, c Credentials) error {
	config, err := json.Marshal(a.Config)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	encrypted := s.aead.Seal(nonce, nonce, plain, []byte(a.ID))
	_, err = s.db.ExecContext(ctx, `INSERT INTO accounts VALUES(?,?,?,?,?,?)`, a.ID, a.Name, a.Provider, string(config), encrypted, a.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return errors.New("账号名称已存在")
	}
	return err
}
func (s *Store) Get(ctx context.Context, id string) (Account, Credentials, error) {
	var a Account
	var config, created string
	var secret []byte
	err := s.db.QueryRowContext(ctx, `SELECT id,name,provider,config,secret,created_at FROM accounts WHERE id=?`, id).Scan(&a.ID, &a.Name, &a.Provider, &config, &secret, &created)
	if err != nil {
		return a, Credentials{}, err
	}
	if err = json.Unmarshal([]byte(config), &a.Config); err != nil {
		return a, Credentials{}, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if len(secret) < s.aead.NonceSize() {
		return a, Credentials{}, errors.New("invalid encrypted credentials")
	}
	plain, err := s.aead.Open(nil, secret[:s.aead.NonceSize()], secret[s.aead.NonceSize():], []byte(a.ID))
	if err != nil {
		return a, Credentials{}, err
	}
	var creds Credentials
	err = json.Unmarshal(plain, &creds)
	return a, creds, err
}
func (s *Store) List(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,provider,config,created_at FROM accounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		var a Account
		var config, created string
		if err = rows.Scan(&a.ID, &a.Name, &a.Provider, &config, &created); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(config), &a.Config); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) UpdateCredentials(ctx context.Context, id string, c Credentials) error {
	plain, err := json.Marshal(c)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	secret := s.aead.Seal(nonce, nonce, plain, []byte(id))
	_, err = s.db.ExecContext(ctx, `UPDATE accounts SET secret=? WHERE id=?`, secret, id)
	return err
}
func (s *Store) Remove(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
