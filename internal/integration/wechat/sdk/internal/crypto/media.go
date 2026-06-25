package crypto

import (
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
)

// AesEcbPaddedSize returns the size after PKCS7 padding for AES-ECB.
func AesEcbPaddedSize(rawSize int64) int64 {
	blockSize := int64(aes.BlockSize)
	return ((rawSize + blockSize - 1) / blockSize) * blockSize
}

// RandomHex generates n random bytes encoded as hex.
func RandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// MD5Hex returns the MD5 hash of data as hex string.
func MD5Hex(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}
