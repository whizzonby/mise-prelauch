package money

import "testing"

func TestFormat(t *testing.T) {
	cases := map[int64]string{
		0:         "TTD 0.00",
		5:         "TTD 0.05",
		33000:     "TTD 330.00",
		123456789: "TTD 1,234,567.89",
		-2500:     "-TTD 25.00",
	}
	for in, want := range cases {
		if got := Format(in, "TTD"); got != want {
			t.Errorf("Format(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestPercentAndIncludedTax(t *testing.T) {
	if got := Percent(33000, 1500); got != 4950 {
		t.Errorf("15%% of 330.00 = %d", got)
	}
	if got := Percent(333, 1250); got != 42 { // 41.625 -> 42
		t.Errorf("12.5%% of 3.33 = %d", got)
	}
	// 112.50 gross at 12.5% contains 12.50 of tax.
	if got := IncludedTax(11250, 1250); got != 1250 {
		t.Errorf("included tax = %d, want 1250", got)
	}
	if got := IncludedTax(33000, 1250); got != 3667 { // 330 - 293.33
		t.Errorf("included tax of 330.00 = %d, want 3667", got)
	}
	if got := IncludedTax(33000, 0); got != 0 {
		t.Errorf("no rate: %d", got)
	}
	if got := Divide(33000, 3); got != 11000 {
		t.Errorf("divide = %d", got)
	}
	if got := Divide(40000, 3); got != 13333 {
		t.Errorf("divide = %d", got)
	}
}
