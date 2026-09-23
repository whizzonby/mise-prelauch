package leads

import "testing"

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		in, email, canonical string
		ok                   bool
	}{
		{"  Asha@Example.COM ", "asha@example.com", "asha@example.com", true},
		{"asha+mise@example.com", "asha+mise@example.com", "asha@example.com", true},
		{"a.s.h.a@gmail.com", "a.s.h.a@gmail.com", "asha@gmail.com", true},
		{"asha+x@googlemail.com", "asha+x@googlemail.com", "asha@gmail.com", true},
		// Dots are significant outside Gmail.
		{"a.sha@example.com", "a.sha@example.com", "a.sha@example.com", true},
		{"", "", "", false},
		{"asha", "", "", false},
		{"asha@localhost", "", "", false},
		{"asha@example..com", "", "", false},
		{"Asha <asha@example.com>", "", "", false},
		{"asha@example.com\nbcc: x@example.com", "", "", false},
		{"+tag@example.com", "", "", false},
	}
	for _, c := range cases {
		email, canonical, ok := NormalizeEmail(c.in)
		if ok != c.ok || email != c.email || canonical != c.canonical {
			t.Errorf("NormalizeEmail(%q) = %q, %q, %v; want %q, %q, %v", c.in, email, canonical, ok, c.email, c.canonical, c.ok)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"+1 (868) 555-0100", "+18685550100", true},
		{"868.555.0100", "8685550100", true},
		{"555-0100", "5550100", true},
		{"12345", "", false},
		{"1234567890123456", "", false},
		{"call me", "", false},
		{"868+5550100", "", false},
	}
	for _, c := range cases {
		got, ok := NormalizePhone(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNameValidation(t *testing.T) {
	valid := []string{"Asha", "Jean-Luc", "D'Angelo", "Mary Ann", "Zoë", "José", "St. Clair"}
	invalid := []string{"", "http://x.example", "<b>Asha</b>", "Asha!", "Win $100", "1234", "-Asha"}
	for _, name := range valid {
		if _, err := validateSignup(validInput(name)); err != nil {
			t.Errorf("name %q rejected: %v", name, err)
		}
	}
	for _, name := range invalid {
		if _, err := validateSignup(validInput(name)); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
}

func validInput(name string) SignupInput {
	return SignupInput{FirstName: name, Email: "asha@example.com", Location: "port-of-spain", Consent: true, ElapsedMS: 5000}
}
