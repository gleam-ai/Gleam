package credentials

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gleam/internal/atomicfile"
)

const File = "credentials.json"

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
	path string
	mu   sync.Mutex
}

type file struct {
	LLMAPIKey     string        `json:"llm_api_key,omitempty"`
	LLMAPIKeyHost string        `json:"llm_api_key_host,omitempty"`
	Cloud         *CloudSession `json:"cloud,omitempty"`
	CloudCfg      *CloudConfig  `json:"cloud_config,omitempty"`
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(dataDir, File)}, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) read() (*file, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	data, err = decrypt(data)
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
	data, err = encrypt(data)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o600)
}

func (s *Store) ResolveLLMAPIKey(host string) (key, storedHost string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil || f.LLMAPIKey == "" {
		return "", ""
	}
	if f.LLMAPIKeyHost == "" && host != "" {
		f.LLMAPIKeyHost = host
		if err := s.write(f); err != nil {
			return f.LLMAPIKey, host
		}
		return f.LLMAPIKey, host
	}
	if host != "" && f.LLMAPIKeyHost != host {
		return "", f.LLMAPIKeyHost
	}
	return f.LLMAPIKey, f.LLMAPIKeyHost
}

func (s *Store) SetLLMAPIKey(key, host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if f == nil {
		f = &file{}
	}
	f.LLMAPIKey, f.LLMAPIKeyHost = key, host
	return s.write(f)
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if f == nil {
		f = &file{}
	}
	f.Cloud = sess
	return s.write(f)
}

func (s *Store) ClearSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	f.Cloud = nil
	return s.write(f)
}
