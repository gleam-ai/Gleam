package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"gleam/internal/atomicfile"
)

// Non-Windows at-rest encryption: AES-256-GCM under a 32-byte master key. The
// key lives in the OS keyring when one is reachable, otherwise in a 0600 key
// file in the data dir. Every blob tags which source sealed it (blob[0]) so a
// reader loads the matching key instead of silently substituting another.
//
// This file is untagged so the AES/key-file logic is exercised on every CI
// platform (Windows CI has no keyring, so it drives the key-file path here even
// though the Windows production binary uses DPAPI and the linker drops this).
const (
	masterKeySize = 32
	nonceSize     = 12
	gcmTagSize    = 16
	keyFileName   = "credentials.key"

	srcKeyring byte = 0
	srcKeyfile byte = 1
)

var (
	errShortBlob    = errors.New("credentials: 密文长度不足")
	errBadMasterKey = errors.New("credentials: 主密钥长度非法（应为 32 字节）")
	errBadSource    = errors.New("credentials: 未知的密钥来源标记")
)

func genMasterKey() ([]byte, error) {
	key := make([]byte, masterKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// sealBlob lays out [src][nonce][ciphertext+tag]. The src byte is authenticated
// implicitly: it selects the key, and a wrong key fails the GCM tag check.
func sealBlob(plain, key []byte, src byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	blob := make([]byte, 0, 1+nonceSize+len(plain)+gcmTagSize)
	blob = append(blob, src)
	blob = append(blob, nonce...)
	return gcm.Seal(blob, nonce, plain, nil), nil
}

func blobSource(blob []byte) (byte, error) {
	if len(blob) < 1 {
		return 0, errShortBlob
	}
	return blob[0], nil
}

func openBlob(blob, key []byte) ([]byte, error) {
	if len(blob) < 1+nonceSize+gcmTagSize {
		return nil, errShortBlob
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := blob[1 : 1+nonceSize]
	return gcm.Open(nil, nonce, blob[1+nonceSize:], nil)
}

func keyFilePath(dir string) string { return filepath.Join(dir, keyFileName) }

func loadKeyFile(dir string) ([]byte, error) {
	key, err := os.ReadFile(keyFilePath(dir))
	if err != nil {
		return nil, err
	}
	if len(key) != masterKeySize {
		return nil, errBadMasterKey
	}
	return key, nil
}

func storeKeyFile(dir string, key []byte) error {
	return atomicfile.Write(keyFilePath(dir), key, 0o600)
}

// cache records the resolved master key. Callers hold p.mu.
func (p *protector) cache(key []byte, src byte) {
	p.key, p.src, p.have = key, src, true
}

// keyFileKey loads the durable fallback key, creating it on first use. A present
// but unreadable/corrupt key file is surfaced, never silently overwritten.
func (p *protector) keyFileKey() ([]byte, error) {
	key, err := loadKeyFile(p.keyDir)
	if err == nil {
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key, err = genMasterKey()
	if err != nil {
		return nil, err
	}
	if err := storeKeyFile(p.keyDir, key); err != nil {
		return nil, err
	}
	return key, nil
}

// masterKeyForWrite picks the key for sealing a new blob. The cache is checked
// first: a read-modify-write cycle already loaded the existing key during read,
// so the write reuses it and can never orphan the current ciphertext. A fresh
// key is minted only when there was no readable blob (first run / plaintext
// migration). Keyring is best-effort; if it is absent or unusable (headless
// Linux with no secret-service daemon), the 0600 key file is the durable fallback.
func (p *protector) masterKeyForWrite() ([]byte, byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.have {
		return p.key, p.src, nil
	}
	if keyringAvailable() {
		if key, err := keyringLoad(); err == nil && len(key) == masterKeySize {
			p.cache(key, srcKeyring)
			return key, srcKeyring, nil
		}
		key, err := genMasterKey()
		if err != nil {
			return nil, 0, err
		}
		if err := keyringStore(key); err == nil {
			p.cache(key, srcKeyring)
			return key, srcKeyring, nil
		}
	}
	key, err := p.keyFileKey()
	if err != nil {
		return nil, 0, err
	}
	p.cache(key, srcKeyfile)
	return key, srcKeyfile, nil
}

// masterKeyForRead loads the key tagged by an existing blob. It never creates a
// key: a keyring-tagged blob whose keyring is now unreachable is a clear error,
// not a silent fall back to a different key (which would fail the tag anyway).
func (p *protector) masterKeyForRead(src byte) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.have && p.src == src {
		return p.key, nil
	}
	switch src {
	case srcKeyring:
		if !keyringAvailable() {
			return nil, errKeyringUnavailable
		}
		key, err := keyringLoad()
		if err != nil {
			return nil, err
		}
		if len(key) != masterKeySize {
			return nil, errBadMasterKey
		}
		p.cache(key, srcKeyring)
		return key, nil
	case srcKeyfile:
		key, err := loadKeyFile(p.keyDir)
		if err != nil {
			return nil, err
		}
		p.cache(key, srcKeyfile)
		return key, nil
	default:
		return nil, errBadSource
	}
}
