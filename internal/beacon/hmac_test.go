package beacon

import (
	"testing"
)

func TestComputeHMAC(t *testing.T) {
	data := []byte("test payload data")
	secret := "test-secret-key"

	sig := ComputeHMAC(data, secret)

	if len(sig) != HMACSize {
		t.Fatalf("expected HMAC size %d, got %d", HMACSize, len(sig))
	}

	// Same data and secret should produce the same HMAC
	sig2 := ComputeHMAC(data, secret)
	for i := range sig {
		if sig[i] != sig2[i] {
			t.Fatalf("HMAC not deterministic at byte %d", i)
		}
	}
}

func TestVerifyHMAC_Valid(t *testing.T) {
	data := []byte("test payload data")
	secret := "test-secret-key"

	sig := ComputeHMAC(data, secret)

	if !VerifyHMAC(sig, data, secret) {
		t.Fatal("expected HMAC to verify successfully")
	}
}

func TestVerifyHMAC_InvalidSecret(t *testing.T) {
	data := []byte("test payload data")
	sig := ComputeHMAC(data, "correct-secret")

	if VerifyHMAC(sig, data, "wrong-secret") {
		t.Fatal("expected HMAC verification to fail with wrong secret")
	}
}

func TestVerifyHMAC_TamperedData(t *testing.T) {
	data := []byte("test payload data")
	secret := "test-secret-key"
	sig := ComputeHMAC(data, secret)

	tampered := []byte("modified payload data")
	if VerifyHMAC(sig, tampered, secret) {
		t.Fatal("expected HMAC verification to fail with tampered data")
	}
}

func TestVerifyHMAC_TruncatedSignature(t *testing.T) {
	data := []byte("test payload data")
	secret := "test-secret-key"
	sig := ComputeHMAC(data, secret)

	// Truncate signature
	if VerifyHMAC(sig[:16], data, secret) {
		t.Fatal("expected HMAC verification to fail with truncated signature")
	}
}
