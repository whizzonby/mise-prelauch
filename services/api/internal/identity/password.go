package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id with the OWASP baseline parameters (19 MiB, 2 passes, 1 lane).
// The parameters are stored in each hash, so they can be raised later and old
// hashes still verify. Carried over unchanged from the pre-launch admin.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

const (
	MinPasswordLength = 12
	// MaxPasswordBytes bounds the work an attacker can make the hasher do.
	MaxPasswordBytes = 256
)

// CheckPassword returns a user-facing problem with a new password, or "".
func CheckPassword(password string) string {
	switch {
	case len([]rune(password)) < MinPasswordLength:
		return fmt.Sprintf("Use at least %d characters.", MinPasswordLength)
	case len(password) > MaxPasswordBytes:
		return "Use a shorter password."
	case strings.TrimSpace(password) == "":
		return "Use letters, numbers or symbols, not only spaces."
	}
	return ""
}

// HashPassword returns an Argon2id hash in PHC string format.
func HashPassword(password string) (string, error) {
	if problem := CheckPassword(password); problem != "" {
		return "", fmt.Errorf("password rejected: %s", problem)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword checks a password against an encoded hash in constant time.
func VerifyPassword(encoded, password string) bool {
	if len(password) > MaxPasswordBytes {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	var memory, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false
	}
	// Refuse parameters far outside what this code writes, so a corrupted or
	// planted hash cannot make one verification take minutes.
	if memory > 256*1024 || passes > 10 || threads > 8 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) < 16 || len(want) > 64 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is checked when an email is unknown, so a sign-in takes the same
// time whether or not the account exists.
var dummyHash, _ = HashPassword("mise-timing-equaliser-password")
