// Package token issues and checks stateless, signed, expiring tokens. They let
// a lead prove who they are from an email link or the browser that signed up,
// without a password or a token table.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

type Purpose string

const (
	PurposeVerifyEmail Purpose = "verify"
	PurposeProfile     Purpose = "profile"
	PurposeUnsubscribe Purpose = "unsub"
)

var (
	ErrInvalid = errors.New("token is invalid")
	ErrExpired = errors.New("token has expired")
)

type Signer struct {
	key []byte
	now func() time.Time
}

func NewSigner(key []byte) *Signer {
	return &Signer{key: key, now: time.Now}
}

// Sign returns a token binding subject to purpose until ttl elapses. A ttl of
// zero means the token does not expire (used for unsubscribe links, which must
// keep working for as long as the email exists).
func (s *Signer) Sign(purpose Purpose, subject string, ttl time.Duration) string {
	var exp int64
	if ttl > 0 {
		exp = s.now().Add(ttl).Unix()
	}
	payload := string(purpose) + "|" + subject + "|" + strconv.FormatInt(exp, 10)
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(s.mac(payload))
}

// Verify returns the subject if the token is authentic, unexpired and was
// issued for the given purpose.
func (s *Signer) Verify(purpose Purpose, tok string) (string, error) {
	enc := base64.RawURLEncoding
	payloadPart, sigPart, ok := strings.Cut(tok, ".")
	if !ok || len(tok) > 512 {
		return "", ErrInvalid
	}
	payload, err := enc.DecodeString(payloadPart)
	if err != nil {
		return "", ErrInvalid
	}
	sig, err := enc.DecodeString(sigPart)
	if err != nil || !hmac.Equal(sig, s.mac(string(payload))) {
		return "", ErrInvalid
	}
	parts := strings.Split(string(payload), "|")
	if len(parts) != 3 || parts[0] != string(purpose) {
		return "", ErrInvalid
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", ErrInvalid
	}
	if exp != 0 && s.now().Unix() > exp {
		return "", ErrExpired
	}
	return parts[1], nil
}

func (s *Signer) mac(payload string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("mise-token-v1\x00" + payload))
	return m.Sum(nil)
}

// HashIP returns a keyed hash of an IP address. It lets abuse rules compare
// "same network" without the database ever holding the address itself.
func (s *Signer) HashIP(ip string) string {
	if ip == "" {
		return ""
	}
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("mise-ip-v1\x00" + ip))
	return hex.EncodeToString(m.Sum(nil)[:16])
}
