// Package crypto seals small secrets for storage in the database, using
// AES-256-GCM from the standard library. It invents nothing: it chooses a
// nonce, binds the ciphertext to the row it belongs to, and versions the
// format so the key can be rotated later.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// version 1: 0x01 || 12-byte nonce || AES-256-GCM ciphertext and tag.
const version1 byte = 1

var ErrUnreadable = errors.New("sealed value cannot be opened")

type Sealer struct {
	aead cipher.AEAD
}

// NewSealer takes a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext. context (for example the owning user id) is
// authenticated but not stored: a value copied onto another row will not open.
func (s *Sealer) Seal(plaintext, context []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+s.aead.Overhead())
	out = append(out, version1)
	out = append(out, nonce...)
	return s.aead.Seal(out, nonce, plaintext, context), nil
}

func (s *Sealer) Open(sealed, context []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < 1+n+s.aead.Overhead() || sealed[0] != version1 {
		return nil, ErrUnreadable
	}
	plain, err := s.aead.Open(nil, sealed[1:1+n], sealed[1+n:], context)
	if err != nil {
		return nil, ErrUnreadable
	}
	return plain, nil
}
