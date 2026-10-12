package credentials

import "encoding/base64"

const magic = "gleam-enc:v1:"

func encrypt(plain []byte) ([]byte, error) {
	blob, err := protectBytes(plain)
	if err != nil {
		return nil, err
	}
	if blob == nil {
		return plain, nil
	}
	return []byte(magic + base64.StdEncoding.EncodeToString(blob)), nil
}

func decrypt(data []byte) ([]byte, error) {
	raw := string(data)
	if len(raw) <= len(magic) || raw[:len(magic)] != magic {
		return data, nil
	}
	blob, err := base64.StdEncoding.DecodeString(raw[len(magic):])
	if err != nil {
		return nil, err
	}
	return unprotectBytes(blob)
}
