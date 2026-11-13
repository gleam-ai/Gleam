//go:build !windows

package credentials

func (p *protector) protectBytes(plain []byte) ([]byte, error) {
	key, src, err := p.masterKeyForWrite()
	if err != nil {
		return nil, err
	}
	return sealBlob(plain, key, src)
}

func (p *protector) unprotectBytes(blob []byte) ([]byte, error) {
	src, err := blobSource(blob)
	if err != nil {
		return nil, err
	}
	key, err := p.masterKeyForRead(src)
	if err != nil {
		return nil, err
	}
	return openBlob(blob, key)
}
