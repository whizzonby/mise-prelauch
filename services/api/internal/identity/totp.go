package identity

import (
	"bytes"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"image/png"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTP (RFC 6238) through github.com/pquerna/otp: 6 digits, 30-second steps,
// SHA-1, which is what every authenticator app supports. One step of clock
// drift is accepted either side. The step that matched is returned so the
// caller can refuse to accept it twice.

const (
	totpPeriod = 30
	totpSkew   = 1
)

var totpOpts = totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

type totpKey struct {
	Secret     string
	OTPAuthURL string
	QRPNG      string // base64 PNG of the otpauth URL
}

func newTOTPKey(issuer, account string) (totpKey, error) {
	return totpKeyFor(issuer, account, nil)
}

// totpKeyFor builds the key for an existing base32 secret, or a new random
// one when secret is nil.
func totpKeyFor(issuer, account string, secret []byte) (totpKey, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      totpPeriod,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
		Secret:      secret,
	})
	if err != nil {
		return totpKey{}, err
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return totpKey{}, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return totpKey{}, err
	}
	return totpKey{Secret: key.Secret(), OTPAuthURL: key.URL(), QRPNG: base64.StdEncoding.EncodeToString(buf.Bytes())}, nil
}

// existingTOTPKey rebuilds the key (and QR code) for a stored base32 secret.
func existingTOTPKey(issuer, account, secret string) (totpKey, error) {
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return totpKey{}, err
	}
	return totpKeyFor(issuer, account, raw)
}

// matchTOTP returns the time step a code is valid for, or ok=false.
func matchTOTP(secret, code string, now time.Time) (step int64, ok bool) {
	if len(code) != 6 {
		return 0, false
	}
	current := now.Unix() / totpPeriod
	for offset := int64(-totpSkew); offset <= totpSkew; offset++ {
		s := current + offset
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*totpPeriod, 0), totpOpts)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return s, true
		}
	}
	return 0, false
}
