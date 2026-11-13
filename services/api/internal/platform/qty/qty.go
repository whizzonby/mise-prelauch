// Package qty is exact decimal arithmetic for ingredient quantities.
//
// Quantities are never binary floating point: a value is a rational number
// (math/big.Rat) parsed from and written as a decimal string, and stored in
// PostgreSQL as numeric. Scaling a recipe (×kits ÷yield ÷usable share) stays
// exact; rounding happens once, half away from zero, when a figure is shown
// or stored.
package qty

import (
	"fmt"
	"math/big"
	"strings"
)

// Q is an exact quantity. The zero value is 0.
type Q struct{ r big.Rat }

// Parse reads a plain decimal ("12", "0.5", "1250.125"). Exponents,
// fractions and signs other than a leading minus are refused.
func Parse(s string) (Q, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Q{}, fmt.Errorf("qty: empty")
	}
	body := strings.TrimPrefix(s, "-")
	dot := false
	digits := 0
	for _, c := range body {
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && !dot:
			dot = true
		default:
			return Q{}, fmt.Errorf("qty: %q is not a decimal number", s)
		}
	}
	if digits == 0 || digits > 30 {
		return Q{}, fmt.Errorf("qty: %q is not a decimal number", s)
	}
	var q Q
	if _, ok := q.r.SetString(s); !ok {
		return Q{}, fmt.Errorf("qty: %q is not a decimal number", s)
	}
	return q, nil
}

// MustParse is Parse for constants and tests.
func MustParse(s string) Q {
	q, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return q
}

// Int is a whole number.
func Int(n int64) Q {
	var q Q
	q.r.SetInt64(n)
	return q
}

func (a Q) Add(b Q) Q { var q Q; q.r.Add(&a.r, &b.r); return q }
func (a Q) Sub(b Q) Q { var q Q; q.r.Sub(&a.r, &b.r); return q }
func (a Q) Mul(b Q) Q { var q Q; q.r.Mul(&a.r, &b.r); return q }

// Div divides; it panics on division by zero, which callers rule out first.
func (a Q) Div(b Q) Q {
	if b.r.Sign() == 0 {
		panic("qty: division by zero")
	}
	var q Q
	q.r.Quo(&a.r, &b.r)
	return q
}

func (a Q) Sign() int     { return a.r.Sign() }
func (a Q) Cmp(b Q) int   { return a.r.Cmp(&b.r) }
func (a Q) IsZero() bool  { return a.r.Sign() == 0 }
func (a Q) Rat() *big.Rat { return new(big.Rat).Set(&a.r) }

// Round returns the value rounded half away from zero to places decimals.
func (a Q) Round(places int) Q {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
	num := new(big.Int).Mul(a.r.Num(), scale)
	den := a.r.Denom()
	quo, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	// |rem|*2 >= den rounds away from zero.
	if new(big.Int).Mul(new(big.Int).Abs(rem), big.NewInt(2)).Cmp(den) >= 0 {
		if num.Sign() < 0 {
			quo.Sub(quo, big.NewInt(1))
		} else {
			quo.Add(quo, big.NewInt(1))
		}
	}
	var q Q
	q.r.SetFrac(quo, scale)
	return q
}

// String writes the value rounded to 3 places, without trailing zeros
// ("1250", "0.5", "333.333").
func (a Q) String() string { return a.Format(3) }

// Format writes the value rounded to places decimals, trimming trailing zeros.
func (a Q) Format(places int) string {
	rounded := a.Round(places)
	s := rounded.r.FloatString(places)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

// MarshalJSON writes the quantity as a JSON string, so no client parses it
// as a binary float.
func (a Q) MarshalJSON() ([]byte, error) { return []byte(`"` + a.String() + `"`), nil }

// UnmarshalJSON accepts a JSON string or number holding a plain decimal.
func (a *Q) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	q, err := Parse(s)
	if err != nil {
		return err
	}
	*a = q
	return nil
}
