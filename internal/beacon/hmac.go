package beacon

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// HMACSize is the length of the HMAC-SHA256 signature in bytes.
const HMACSize = 32

// ComputeHMAC returns the HMAC-SHA256 signature for the given data using the shared secret.
func ComputeHMAC(data []byte, secret string) []byte {
	key, _ := hex.DecodeString(secret)
	// If hex decode fails, fallback to raw bytes or handle error
	if len(key) == 0 {
		key = []byte(secret)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// VerifyHMAC performs a constant-time comparison of the expected HMAC against the provided signature.
func VerifyHMAC(sig, data []byte, secret string) bool {
	expected := ComputeHMAC(data, secret)
	return hmac.Equal(sig, expected)
}
