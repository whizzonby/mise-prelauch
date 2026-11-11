package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("unexpected hash format %q", hash)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Error("right password rejected")
	}
	if VerifyPassword(hash, "correct horse battery stapler") {
		t.Error("wrong password accepted")
	}
	again, _ := HashPassword("correct horse battery staple")
	if again == hash {
		t.Error("two hashes of one password are identical: salt not random")
	}
}

func TestCheckPassword(t *testing.T) {
	for pw, ok := range map[string]bool{
		"short":                    false,
		"            ":             false,
		"twelve chars":             true,
		"ünïcödé pässwörd":         true,
		strings.Repeat("a", 257):   false,
		"correct horse battery 99": true,
	} {
		if got := CheckPassword(pw) == ""; got != ok {
			t.Errorf("CheckPassword(%q) ok = %v, want %v", pw, got, ok)
		}
	}
	if _, err := HashPassword("short"); err == nil {
		t.Error("HashPassword accepted a short password")
	}
}

func TestVerifyPasswordRejectsHostileHashes(t *testing.T) {
	for _, h := range []string{
		"", "plain", "$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		// Absurd cost parameters must not be honoured.
		"$argon2id$v=19$m=99999999,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g",
	} {
		if VerifyPassword(h, "anything at all") {
			t.Errorf("VerifyPassword accepted %q", h)
		}
	}
	if VerifyPassword(dummyHash, strings.Repeat("x", MaxPasswordBytes+1)) {
		t.Error("oversized password accepted")
	}
}

func TestMatchTOTP(t *testing.T) {
	key, err := newTOTPKey("Mise", "ada@mise.test")
	if err != nil {
		t.Fatal(err)
	}
	if key.QRPNG == "" || !strings.Contains(key.OTPAuthURL, "issuer=Mise") {
		t.Errorf("key = %+v", key)
	}
	now := time.Unix(1_800_000_000, 0)
	code, _ := totp.GenerateCode(key.Secret, now)
	step, ok := matchTOTP(key.Secret, code, now)
	if !ok || step != now.Unix()/30 {
		t.Fatalf("current code: step %d ok %v", step, ok)
	}
	// One step of drift either way is accepted, two are not.
	if _, ok := matchTOTP(key.Secret, code, now.Add(30*time.Second)); !ok {
		t.Error("code from the previous step rejected")
	}
	if _, ok := matchTOTP(key.Secret, code, now.Add(90*time.Second)); ok {
		t.Error("code from three steps ago accepted")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := matchTOTP(key.Secret, bad, now); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestPrincipal(t *testing.T) {
	p := &Principal{FirstName: "Ada", LastName: "Lovelace", Roles: []string{"marketing", "support"}, Permissions: []Permission{PermLeadsRead}}
	if p.Name() != "Ada Lovelace" || p.PrimaryRole() != "marketing" {
		t.Errorf("name %q role %q", p.Name(), p.PrimaryRole())
	}
	if !p.Can(PermLeadsRead) || p.Can(PermLeadsErase) {
		t.Error("Can is wrong")
	}
	for state, complete := range map[MFAState]bool{
		MFAVerified: true, MFANotEnrolled: true, MFANotApplicable: true,
		MFAChallenge: false, MFAEnrolmentRequired: false,
	} {
		if state.Complete() != complete {
			t.Errorf("%s.Complete() = %v", state, !complete)
		}
	}
}
