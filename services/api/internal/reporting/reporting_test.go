package reporting

import "testing"

func TestCSVCellsCannotRunFormulas(t *testing.T) {
	cases := map[string]string{"=HYPERLINK(1)": "'=HYPERLINK(1)", "+1": "'+1", "-2": "'-2", "@SUM": "'@SUM", "plain": "plain", "": ""}
	for in, want := range cases {
		if got := csvCell(in); got != want {
			t.Errorf("csvCell(%q) = %q, want %q", in, got, want)
		}
	}
}
