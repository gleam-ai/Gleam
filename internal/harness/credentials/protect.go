package credentials

import (
	"encoding/base64"
	"sync"
)

const magic = "gleam-enc:v1:"

// protector seals/open the credentials blob for one data dir. Windows uses DPAPI
// (user-bound, no key file); other platforms use AES-256-GCM with a master key
// from the OS keyring, falling back to a 0600 key file in keyDir. The cached
// master key (key/src/have) is only used by the non-Windows path and is guarded
// by mu; callers also hold Store.mu, so mu is a cheap self-contained second layer.
type protector struct {
	keyDir string

	mu   sync.Mutex
	key  []byte
	src  byte
	have bool
}

func newProtector(keyDir string) *protector { return &protector{keyDir: keyDir} }

func (p *protector) encrypt(plain []byte) ([]byte, error) {
	blob, err := p.protectBytes(plain)
	if err != nil {
		return nil, err
	}
	if blob == nil {
		return plain, nil
	}
	return []byte(magic + base64.StdEncoding.EncodeToString(blob)), nil
}

func (p *protector) decrypt(data []byte) ([]byte, error) {
	raw := string(data)
	if len(raw) <= len(magic) || raw[:len(magic)] != magic {
		return data, nil
	}
	blob, err := base64.StdEncoding.DecodeString(raw[len(magic):])
	if err != nil {
		return nil, err
	}
	return p.unprotectBytes(blob)
}
