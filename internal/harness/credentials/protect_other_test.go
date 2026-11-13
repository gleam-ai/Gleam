//go:build !windows

package credentials

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestProtectorAESRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := newProtector(dir)
	key, err := genMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	// Force the durable key-file path so the test never touches a real keyring.
	if err := storeKeyFile(dir, key); err != nil {
		t.Fatal(err)
	}
	p.cache(key, srcKeyfile)

	plain := []byte(`{"llm_api_keys":{"open.bigmodel.cn":"sk-secret"}}`)
	enc, err := p.encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(enc, []byte(magic)) {
		t.Fatalf("密文应带 magic 前缀，头部 = %.20q", enc)
	}
	if bytes.Contains(enc, []byte("sk-secret")) {
		t.Error("明文 key 不应出现在密文里")
	}
	dec, err := p.decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, plain) {
		t.Errorf("round trip = %q, want %q", dec, plain)
	}
}

func TestProtectorPlaintextPassthrough(t *testing.T) {
	p := newProtector(t.TempDir())
	plain := []byte(`{"llm_api_key":"sk-legacy"}`)
	dec, err := p.decrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dec, plain) {
		t.Error("无 magic 前缀的旧明文应原样透传")
	}
}

func TestProtectorKeyfileTaggedDecrypt(t *testing.T) {
	dir := t.TempDir()
	key, _ := genMasterKey()
	if err := storeKeyFile(dir, key); err != nil {
		t.Fatal(err)
	}
	// A cold protector (empty cache) must resolve the key from the blob's tag.
	p := newProtector(dir)
	blob, err := sealBlob([]byte("payload"), key, srcKeyfile)
	if err != nil {
		t.Fatal(err)
	}
	enc := []byte(magic + base64.StdEncoding.EncodeToString(blob))
	dec, err := p.decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != "payload" {
		t.Errorf("解密 = %q, want payload", dec)
	}
}
