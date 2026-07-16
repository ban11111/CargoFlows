package secrets

import (
	"bytes"
	"testing"
)

func TestAESGCMRoundTripUsesUniqueNonce(t *testing.T) {
	box, err := NewAESGCM(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	a, err := box.Seal([]byte("sk-proj-secret"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := box.Seal([]byte("sk-proj-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Nonce, b.Nonce) {
		t.Fatal("nonce was reused")
	}
	if a.KeyVersion != "v1" {
		t.Fatalf("key version = %q, want v1", a.KeyVersion)
	}
	plain, err := box.Open(a)
	if err != nil || string(plain) != "sk-proj-secret" {
		t.Fatalf("open = %q, %v", plain, err)
	}
}

func TestAESGCMRejectsTamperingAndWrongLength(t *testing.T) {
	if _, err := NewAESGCM(make([]byte, 31)); err == nil {
		t.Fatal("accepted 31-byte key")
	}
	box, err := NewAESGCM(bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	value, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	value.Ciphertext[0] ^= 0xff
	if _, err := box.Open(value); err == nil {
		t.Fatal("accepted tampered ciphertext")
	}
}

func TestAESGCMRejectsWrongKey(t *testing.T) {
	box, err := NewAESGCM(bytes.Repeat([]byte{0x33}, 32))
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewAESGCM(bytes.Repeat([]byte{0x44}, 32))
	if err != nil {
		t.Fatal(err)
	}
	value, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(value); err == nil {
		t.Fatal("accepted ciphertext encrypted with another key")
	}
}

func TestAESGCMRejectsInvalidNonceLength(t *testing.T) {
	box, err := NewAESGCM(bytes.Repeat([]byte{0x55}, 32))
	if err != nil {
		t.Fatal(err)
	}
	value, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	value.Nonce = value.Nonce[:len(value.Nonce)-1]
	if _, err := box.Open(value); err == nil {
		t.Fatal("accepted invalid nonce length")
	}
}
