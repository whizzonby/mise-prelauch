// Package email normalises addresses. Leads and accounts share it, so the
// same inbox always produces the same canonical form and a customer account
// can be matched to the waitlist lead it came from.
package email

import (
	"net/mail"
	"strings"
	"unicode"
)

// Normalize returns the deliverable form (trimmed, lower-cased) and the
// canonical form used for uniqueness: no "+tag", and for Gmail no dots.
func Normalize(raw string) (address, canonical string, ok bool) {
	address = strings.ToLower(strings.TrimSpace(raw))
	if address == "" || len(address) > 254 {
		return "", "", false
	}
	addr, err := mail.ParseAddress(address)
	if err != nil || addr.Address != address {
		return "", "", false
	}
	at := strings.LastIndex(address, "@")
	local, domain := address[:at], address[at+1:]
	if len(local) > 64 || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", "", false
	}
	for _, r := range address {
		if r > unicode.MaxASCII || unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", "", false
		}
	}

	canonLocal, _, _ := strings.Cut(local, "+")
	canonDomain := domain
	if domain == "gmail.com" || domain == "googlemail.com" {
		canonLocal = strings.ReplaceAll(canonLocal, ".", "")
		canonDomain = "gmail.com"
	}
	if canonLocal == "" {
		return "", "", false
	}
	return address, canonLocal + "@" + canonDomain, true
}
