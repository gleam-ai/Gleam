//go:build !windows

package credentials

import "errors"

func protectBytes(plain []byte) ([]byte, error) { return nil, nil }

func unprotectBytes(blob []byte) ([]byte, error) {
	return nil, errors.New("credentials: 文件已加密，但本平台没有 DPAPI 可解密")
}
