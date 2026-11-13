package credentials

import (
	"bytes"
	"os"
	"runtime"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	key, err := genMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"llm_api_keys":{"open.bigmodel.cn":"sk-secret"}}`)
	for _, src := range []byte{srcKeyring, srcKeyfile} {
		blob, err := sealBlob(plain, key, src)
		if err != nil {
			t.Fatal(err)
		}
		gotSrc, err := blobSource(blob)
		if err != nil {
			t.Fatal(err)
		}
		if gotSrc != src {
			t.Errorf("src tag = %d, want %d", gotSrc, src)
		}
		out, err := openBlob(blob, key)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, plain) {
			t.Errorf("round trip = %q, want %q", out, plain)
		}
	}
}

func TestSealIsNonDeterministic(t *testing.T) {
	key, _ := genMasterKey()
	plain := []byte("same plaintext")
	a, err := sealBlob(plain, key, srcKeyfile)
	if err != nil {
		t.Fatal(err)
	}
	b, err := sealBlob(plain, key, srcKeyfile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Error("两次密封应使用不同 nonce，密文不应相同")
	}
}

func TestOpenDetectsTamper(t *testing.T) {
	key, _ := genMasterKey()
	blob, err := sealBlob([]byte("attack at dawn"), key, srcKeyfile)
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-1] ^= 0xFF
	if _, err := openBlob(blob, key); err == nil {
		t.Error("篡改后 GCM 认证应失败")
	}
}

func TestOpenWrongKeyFails(t *testing.T) {
	key, _ := genMasterKey()
	other, _ := genMasterKey()
	blob, err := sealBlob([]byte("secret"), key, srcKeyring)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openBlob(blob, other); err == nil {
		t.Error("错误密钥应解密失败")
	}
}

func TestOpenShortBlob(t *testing.T) {
	key, _ := genMasterKey()
	if _, err := openBlob([]byte{srcKeyfile}, key); err == nil {
		t.Error("过短密文应报错")
	}
	if _, err := blobSource(nil); err == nil {
		t.Error("空 blob 取来源应报错")
	}
}

func TestGenMasterKeyRandom(t *testing.T) {
	a, err := genMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := genMasterKey()
	if len(a) != masterKeySize {
		t.Fatalf("key len = %d, want %d", len(a), masterKeySize)
	}
	if bytes.Equal(a, b) {
		t.Error("两次生成的主密钥不应相同")
	}
}

func TestKeyFileStoreLoad(t *testing.T) {
	dir := t.TempDir()
	key, _ := genMasterKey()
	if err := storeKeyFile(dir, key); err != nil {
		t.Fatal(err)
	}
	got, err := loadKeyFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, key) {
		t.Error("key file round trip 失败")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(keyFilePath(dir))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("key file 权限 = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestLoadKeyFileBadLength(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(keyFilePath(dir), []byte("too-short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKeyFile(dir); err == nil {
		t.Error("非法长度主密钥应报错")
	}
}

func TestKeyFileKeyCreatesOnce(t *testing.T) {
	dir := t.TempDir()
	p := newProtector(dir)
	k1, err := p.keyFileKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := p.keyFileKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(k1, k2) {
		t.Error("第二次应复用已落盘的主密钥")
	}
}

func TestMasterKeyForReadKeyfile(t *testing.T) {
	dir := t.TempDir()
	key, _ := genMasterKey()
	if err := storeKeyFile(dir, key); err != nil {
		t.Fatal(err)
	}
	p := newProtector(dir)
	got, err := p.masterKeyForRead(srcKeyfile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, key) {
		t.Error("应按 keyfile 来源取回主密钥")
	}
	got2, err := p.masterKeyForRead(srcKeyfile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got2, key) {
		t.Error("缓存命中应返回同一主密钥")
	}
}

func TestMasterKeyForReadUnknownSource(t *testing.T) {
	p := newProtector(t.TempDir())
	if _, err := p.masterKeyForRead(0xFF); err == nil {
		t.Error("未知来源标记应报错")
	}
}

func TestMasterKeyForWriteCacheReuse(t *testing.T) {
	p := newProtector(t.TempDir())
	key, _ := genMasterKey()
	p.cache(key, srcKeyfile)
	got, src, err := p.masterKeyForWrite()
	if err != nil {
		t.Fatal(err)
	}
	if src != srcKeyfile || !bytes.Equal(got, key) {
		t.Error("有缓存时应直接复用，不重新取密钥")
	}
}

// The keyring-vs-keyfile decision is exercised only where no keyring is present
// (Windows CI). On a dev box with a real keyring we skip, so the test never
// touches the user's actual Gleam master key under the production service/account.
func TestMasterKeyForWriteKeyfileFallback(t *testing.T) {
	if keyringAvailable() {
		t.Skip("本机有系统钥匙串，跳过 keyfile 回退测试以免污染真实主密钥")
	}
	dir := t.TempDir()
	p := newProtector(dir)
	key, src, err := p.masterKeyForWrite()
	if err != nil {
		t.Fatal(err)
	}
	if src != srcKeyfile {
		t.Fatalf("无钥匙串时应回退 keyfile，得到 src=%d", src)
	}
	if len(key) != masterKeySize {
		t.Fatalf("key len = %d", len(key))
	}
	got, err := loadKeyFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, key) {
		t.Error("回退应把主密钥落盘")
	}
	rk, err := p.masterKeyForRead(srcKeyfile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rk, key) {
		t.Error("读路径应取回同一主密钥")
	}
}

func TestMasterKeyForReadKeyringUnavailable(t *testing.T) {
	if keyringAvailable() {
		t.Skip("本机有系统钥匙串，跳过不可用分支")
	}
	p := newProtector(t.TempDir())
	if _, err := p.masterKeyForRead(srcKeyring); err == nil {
		t.Error("钥匙串不可用时读取 keyring 来源应报错")
	}
}
