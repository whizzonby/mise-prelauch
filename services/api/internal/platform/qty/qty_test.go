package qty

import (
	"encoding/json"
	"testing"
)

func TestParseAndFormat(t *testing.T) {
	for in, want := range map[string]string{"12": "12", "0.5": "0.5", "1250.1250": "1250.125", "-3.0005": "-3.001", "007": "7", "0.0004": "0"} {
		q, err := Parse(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := q.String(); got != want {
			t.Errorf("%s = %s, want %s", in, got, want)
		}
	}
	for _, bad := range []string{"", "1e3", "1/2", "abc", "1.2.3", "+1", ".", "NaN", "1,5"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestExactScaling(t *testing.T) {
	// 100 g for a 3-serving yield, scaled to 10 kits of 2 servings, at 85%
	// usable: 100 × 20 / 3 / 0.85 = 784.3137... g. A float64 path drifts in
	// the last places; the rational one rounds once.
	got := MustParse("100").Mul(Int(20)).Div(Int(3)).Div(MustParse("0.85"))
	if got.String() != "784.314" {
		t.Errorf("scaled = %s", got)
	}
	// Thirds add back up exactly.
	third := Int(1).Div(Int(3))
	if !third.Add(third).Add(third).Sub(Int(1)).IsZero() {
		t.Error("1/3 + 1/3 + 1/3 != 1")
	}
	// The classic float trap: 0.1 + 0.2.
	if MustParse("0.1").Add(MustParse("0.2")).Cmp(MustParse("0.3")) != 0 {
		t.Error("0.1 + 0.2 != 0.3")
	}
}

func TestRoundHalfAwayFromZero(t *testing.T) {
	for in, want := range map[string]string{"2.0005": "2.001", "2.0004999": "2", "-2.0005": "-2.001", "0.9995": "1"} {
		if got := MustParse(in).String(); got != want {
			t.Errorf("%s = %s, want %s", in, got, want)
		}
	}
}

func TestJSON(t *testing.T) {
	var v struct{ Q Q }
	if err := json.Unmarshal([]byte(`{"Q":"2.5"}`), &v); err != nil || v.Q.String() != "2.5" {
		t.Fatalf("string: %v %s", err, v.Q)
	}
	if err := json.Unmarshal([]byte(`{"Q":4}`), &v); err != nil || v.Q.String() != "4" {
		t.Fatalf("number: %v %s", err, v.Q)
	}
	if err := json.Unmarshal([]byte(`{"Q":"1e9"}`), &v); err == nil {
		t.Error("exponent accepted")
	}
	b, _ := json.Marshal(v)
	if string(b) != `{"Q":"4"}` {
		t.Errorf("marshal = %s", b)
	}
}
