package crypto

import (
	"crypto/aes"
	"crypto/md5" //nolint:gosec // MD5 is required by the WeChat file-transfer protocol.
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
		return "", fmt.Errorf("generate random hex: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// MD5Hex returns the MD5 hash of data as hex string.
func MD5Hex(data []byte) string {
	h := md5.Sum(data) //nolint:gosec // MD5 is required by the WeChat file-transfer protocol.
	return hex.EncodeToString(h[:])
}
