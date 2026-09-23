package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func newTestSigner() *Signer {
	return NewSigner([]byte("0123456789abcdef0123456789abcdef"))
}

func TestSignVerifyRoundTrip(t *testing.T) {
	s := newTestSigner()
	tok := s.Sign(PurposeVerifyEmail, "lead-1", time.Hour)
	got, err := s.Verify(PurposeVerifyEmail, tok)
	if err != nil || got != "lead-1" {
		t.Fatalf("Verify = %q, %v; want lead-1, nil", got, err)
	}
}

func TestVerifyRejectsWrongPurpose(t *testing.T) {
	s := newTestSigner()
	tok := s.Sign(PurposeUnsubscribe, "lead-1", 0)
	if _, err := s.Verify(PurposeProfile, tok); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	s := newTestSigner()
	tok := s.Sign(PurposeProfile, "lead-1", time.Hour)
	other := s.Sign(PurposeProfile, "lead-2", time.Hour)
	// Splice lead-2's payload onto lead-1's signature.
	forged := strings.Split(other, ".")[0] + "." + strings.Split(tok, ".")[1]
	if _, err := s.Verify(PurposeProfile, forged); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v; want ErrInvalid", err)
	}
	if _, err := NewSigner([]byte("another-key-another-key-another-key")).Verify(PurposeProfile, tok); !errors.Is(err, ErrInvalid) {
		t.Fatalf("different key: err = %v; want ErrInvalid", err)
	}
}

func TestVerifyExpiry(t *testing.T) {
	s := newTestSigner()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	tok := s.Sign(PurposeVerifyEmail, "lead-1", time.Hour)
	forever := s.Sign(PurposeUnsubscribe, "lead-1", 0)

	s.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := s.Verify(PurposeVerifyEmail, tok); !errors.Is(err, ErrExpired) {
		t.Fatalf("err = %v; want ErrExpired", err)
	}
	if _, err := s.Verify(PurposeUnsubscribe, forever); err != nil {
		t.Fatalf("non-expiring token rejected: %v", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	s := newTestSigner()
	for _, tok := range []string{"", ".", "abc", "abc.def", strings.Repeat("a", 600)} {
		if _, err := s.Verify(PurposeProfile, tok); !errors.Is(err, ErrInvalid) {
			t.Errorf("Verify(%q) err = %v; want ErrInvalid", tok, err)
		}
	}
}

func TestHashIPIsStableAndKeyed(t *testing.T) {
	s := newTestSigner()
	if s.HashIP("203.0.113.9") != s.HashIP("203.0.113.9") {
		t.Fatal("hash is not stable")
	}
	if s.HashIP("203.0.113.9") == s.HashIP("203.0.113.10") {
		t.Fatal("different IPs hashed the same")
	}
	if s.HashIP("") != "" {
		t.Fatal("empty IP should hash to empty")
	}
}
