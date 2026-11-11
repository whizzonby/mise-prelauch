// Package money formats and splits amounts held as integer minor units
// (cents). Nothing in the platform stores or computes money as floating point.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Currency is the only currency the platform sells in today.
const Currency = "TTD"

// Format renders 33000 TTD as "TTD 330.00".
func Format(minor int64, currency string) string {
	sign := ""
	if minor < 0 {
		sign = "-"
		minor = -minor
	}
	return fmt.Sprintf("%s%s %s.%02d", sign, currency, group(minor/100), minor%100)
}

func group(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// Percent returns bps basis points of amount, rounded half up (half away
// from zero for the non-negative amounts used here).
func Percent(amount int64, bps int64) int64 {
	return (amount*bps + 5000) / 10000
}

// IncludedTax is the tax contained in a tax-inclusive gross amount at rate
// bps: gross - gross / (1 + rate), rounded half up.
func IncludedTax(gross int64, bps int64) int64 {
	if bps <= 0 {
		return 0
	}
	net := (gross*10000*2 + (10000 + bps)) / ((10000 + bps) * 2)
	return gross - net
}

// Divide splits amount into n near-equal parts, rounded half up.
func Divide(amount int64, n int64) int64 {
	if n <= 0 {
		return 0
	}
	return (amount*2 + n) / (2 * n)
}
