package credentials

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gleam/internal/atomicfile"
	"gleam/internal/filelock"
)

const File = "credentials.json"

// lockFile guards the read-modify-write cycle across processes sharing a data
// dir (desktop sidecar + CLI). lockTimeout is a safety net, not a target: each
// cycle is a brief local write, so contention normally resolves in milliseconds;
// the generous cap only absorbs slow disk/AV latency under heavy concurrent load.
const (
	lockFile    = "credentials.lock"
	lockTimeout = 20 * time.Second
)

var ErrNotFound = errors.New("凭证尚未创建")

type CloudSession struct {
	Provider     string    `json:"provider,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	Email        string    `json:"email,omitempty"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	DisplayName  string    `json:"display_name,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

type CloudConfig struct {
	SupabaseURL     string `json:"supabase_url,omitempty"`
	SupabaseAnonKey string `json:"supabase_anon_key,omitempty"`
}

type Store struct {
	path     string
	lockPath string
	prot     *protector
	mu       sync.Mutex
}

type file struct {
	// LLMAPIKeys is keyed by endpoint host (llm.KeyScope): one key per vendor.
	LLMAPIKeys map[string]string `json:"llm_api_keys,omitempty"`
	// The single-key fields are the legacy format: read only for migration,
	// never written again.
	LLMAPIKey     string        `json:"llm_api_key,omitempty"`
	LLMAPIKeyHost string        `json:"llm_api_key_host,omitempty"`
	Cloud         *CloudSession `json:"cloud,omitempty"`
	CloudCfg      *CloudConfig  `json:"cloud_config,omitempty"`
}

// migrateKeys moves the legacy single-key pair into the per-host map. It
// reports whether anything changed in memory; callers decide when to persist.
func (f *file) migrateKeys() bool {
	if f.LLMAPIKey == "" {
		return false
	}
	if f.LLMAPIKeys == nil {
		f.LLMAPIKeys = map[string]string{}
	}
	if _, ok := f.LLMAPIKeys[f.LLMAPIKeyHost]; !ok {
		f.LLMAPIKeys[f.LLMAPIKeyHost] = f.LLMAPIKey
	}
	f.LLMAPIKey = ""
	return true
}

func (f *file) lastHost() string {
	if f.LLMAPIKeys[f.LLMAPIKeyHost] != "" {
		return f.LLMAPIKeyHost
	}
	return ""
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	return &Store{
		path:     filepath.Join(dataDir, File),
		lockPath: filepath.Join(dataDir, lockFile),
		prot:     newProtector(dataDir),
	}, nil
}

func (s *Store) Path() string { return s.path }

// locked serializes a read-modify-write cycle both in-process (mu) and across
// processes sharing the data dir (advisory file lock), so two sidecars cannot
// clobber each other's keys or session. Pure reads skip it: atomicfile.Write
// already guarantees a reader sees a complete blob, never a torn one.
func (s *Store) locked(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return filelock.WithLock(s.lockPath, lockTimeout, fn)
}

func (s *Store) read() (*file, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	data, err = s.prot.decrypt(data)
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) write(f *file) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data, err = s.prot.encrypt(data)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o600)
}

func (s *Store) ResolveLLMAPIKey(host string) (key, storedHost string) {
	_ = s.locked(func() error {
		f, err := s.read()
		if err != nil {
			return nil
		}
		if f.migrateKeys() {
			// Encrypt the legacy plaintext key on this read instead of leaving it
			// on disk.
			_ = s.write(f)
		}
		if k := f.LLMAPIKeys[host]; k != "" {
			key, storedHost = k, host
			return nil
		}
		// Keys in old files carry no host: bind them to the current host on first
		// use, then stop lending them out.
		if host != "" {
			if k := f.LLMAPIKeys[""]; k != "" {
				delete(f.LLMAPIKeys, "")
				f.LLMAPIKeys[host] = k
				f.LLMAPIKeyHost = host
				_ = s.write(f)
				key, storedHost = k, host
				return nil
			}
			// Older scopes kept explicit default ports (host:443 / host:80);
			// rebind them to the normalized host instead of losing the key.
			for _, legacy := range []string{host + ":443", host + ":80"} {
				if k := f.LLMAPIKeys[legacy]; k != "" {
					delete(f.LLMAPIKeys, legacy)
					f.LLMAPIKeys[host] = k
					f.LLMAPIKeyHost = host
					_ = s.write(f)
					key, storedHost = k, host
					return nil
				}
			}
		}
		storedHost = f.lastHost()
		return nil
	})
	return key, storedHost
}

func (s *Store) SetLLMAPIKey(key, host string) error {
	return s.locked(func() error {
		f, err := s.read()
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if f == nil {
			f = &file{}
		}
		f.migrateKeys()
		if key == "" {
			if host == "" {
				// Empty key + empty host = clear everything (legacy callers).
				f.LLMAPIKeys = nil
				f.LLMAPIKeyHost = ""
			} else {
				delete(f.LLMAPIKeys, host)
				if f.LLMAPIKeyHost == host {
					f.LLMAPIKeyHost = ""
				}
			}
		} else {
			if f.LLMAPIKeys == nil {
				f.LLMAPIKeys = map[string]string{}
			}
			f.LLMAPIKeys[host] = key
			f.LLMAPIKeyHost = host
		}
		return s.write(f)
	})
}

func (s *Store) GetCloudConfig() *CloudConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil || f.CloudCfg == nil {
		return &CloudConfig{}
	}
	return f.CloudCfg
}

func (s *Store) SetCloudConfig(cfg CloudConfig) error {
	return s.locked(func() error {
		f, err := s.read()
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if f == nil {
			f = &file{}
		}
		if f.CloudCfg == nil {
			f.CloudCfg = &CloudConfig{}
		}
		if cfg.SupabaseURL != "" {
			f.CloudCfg.SupabaseURL = cfg.SupabaseURL
		}
		if cfg.SupabaseAnonKey != "" {
			f.CloudCfg.SupabaseAnonKey = cfg.SupabaseAnonKey
		}
		return s.write(f)
	})
}

func (s *Store) GetSession() *CloudSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil || f.Cloud == nil {
		return nil
	}
	return f.Cloud
}

func (s *Store) SetSession(sess *CloudSession) error {
	return s.locked(func() error {
		f, err := s.read()
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if f == nil {
			f = &file{}
		}
		f.Cloud = sess
		return s.write(f)
	})
}

func (s *Store) ClearSession() error {
	return s.locked(func() error {
		f, err := s.read()
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			return err
		}
		f.Cloud = nil
		return s.write(f)
	})
}
