package vocab

import (
	"os"
	"strings"
	"testing"
)

// Every region customers can choose must be a delivery zone (migration
// 00006: Mise delivers across Trinidad and Tobago). A region added here
// without a zone would show "availability pending" forever.
func TestEveryRegionHasADeliveryZone(t *testing.T) {
	sql, err := os.ReadFile("../../migrations/00006_delivery_zones_tt.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(sql), "-- +goose Down")
	for _, r := range Regions {
		if !strings.Contains(up, "('TT', '"+r.Key+"', 'active')") {
			t.Errorf("region %q has no delivery zone in migration 00006", r.Key)
		}
	}
}

func TestContradictions(t *testing.T) {
	for _, bad := range [][]string{{"vegan", "pescatarian"}, {"vegetarian", "pescatarian"}, {NoDietaryPreference, "vegan"}} {
		if Contradiction(bad) == "" {
			t.Errorf("%v accepted", bad)
		}
	}
	for _, ok := range [][]string{{"vegan", "vegetarian"}, {"halal", "no-pork"}, {NoDietaryPreference}, {}} {
		if msg := Contradiction(ok); msg != "" {
			t.Errorf("%v refused: %s", ok, msg)
		}
	}
	if !Satisfies([]string{"vegan"}, "vegetarian") || !Satisfies([]string{"vegetarian"}, "pescatarian") || Satisfies([]string{"pescatarian"}, "vegetarian") {
		t.Error("diet satisfaction rules are wrong")
	}
}
