package beacon

import (
	"crypto/hmac"
	"crypto/sha256"
)

// HMACSize is the length of the HMAC-SHA256 signature in bytes.
const HMACSize = 32

// ComputeHMAC returns the HMAC-SHA256 signature for the given data using the shared secret.
func ComputeHMAC(data []byte, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	return mac.Sum(nil)
}

// VerifyHMAC performs a constant-time comparison of the expected HMAC against the provided signature.
func VerifyHMAC(sig, data []byte, secret string) bool {
	expected := ComputeHMAC(data, secret)
	return hmac.Equal(sig, expected)
}
