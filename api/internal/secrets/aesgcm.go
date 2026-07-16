package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

type EncryptedValue struct {
	Ciphertext []byte
	Nonce      []byte
	KeyVersion string
}

type AESGCM struct {
	aead cipher.AEAD
}

func NewAESGCM(key []byte) (*AESGCM, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes")
	}
	block, err := aes.NewCipher(append([]byte(nil), key...))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCM{aead: aead}, nil
}

func (a *AESGCM) Seal(plaintext []byte) (EncryptedValue, error) {
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return EncryptedValue{}, err
	}
	return EncryptedValue{
		Ciphertext: a.aead.Seal(nil, nonce, plaintext, nil),
		Nonce:      nonce,
		KeyVersion: "v1",
	}, nil
}

func (a *AESGCM) Open(value EncryptedValue) ([]byte, error) {
	if len(value.Nonce) != a.aead.NonceSize() {
		return nil, fmt.Errorf("invalid nonce length")
	}
	return a.aead.Open(nil, value.Nonce, value.Ciphertext, nil)
}
