package crypto

import (
	"bytes"
	"testing"
)

func newTestSealer(t *testing.T) *Sealer {
	t.Helper()
	s, err := NewSealer(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSealOpenRoundTrip(t *testing.T) {
	s := newTestSealer(t)
	sealed, err := s.Seal([]byte("JBSWY3DPEHPK3PXP"), []byte("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("JBSWY3DPEHPK3PXP")) {
		t.Fatal("plaintext visible in sealed value")
	}
	plain, err := s.Open(sealed, []byte("user-1"))
	if err != nil || string(plain) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("open = %q, %v", plain, err)
	}
}

func TestOpenRejectsOtherContextAndTampering(t *testing.T) {
	s := newTestSealer(t)
	sealed, _ := s.Seal([]byte("secret"), []byte("user-1"))
	if _, err := s.Open(sealed, []byte("user-2")); err == nil {
		t.Error("opened with another row's context")
	}
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	if _, err := s.Open(tampered, []byte("user-1")); err == nil {
		t.Error("opened a tampered value")
	}
	if _, err := s.Open([]byte{1, 2, 3}, []byte("user-1")); err == nil {
		t.Error("opened garbage")
	}
	other, _ := NewSealer(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(sealed, []byte("user-1")); err == nil {
		t.Error("opened with another key")
	}
}

func TestSealUsesFreshNonces(t *testing.T) {
	s := newTestSealer(t)
	a, _ := s.Seal([]byte("same"), nil)
	b, _ := s.Seal([]byte("same"), nil)
	if bytes.Equal(a, b) {
		t.Error("two seals of the same value are identical")
	}
}

func TestNewSealerRejectsShortKey(t *testing.T) {
	if _, err := NewSealer([]byte("short")); err == nil {
		t.Error("accepted a short key")
	}
}
