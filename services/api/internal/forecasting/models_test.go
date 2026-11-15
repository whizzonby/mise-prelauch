package forecasting

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestBaselinesCalculateCorrectly(t *testing.T) {
	x := []float64{10, 12, 14, 9, 11, 13, 8, 16}
	cases := []struct {
		m    Method
		want []float64
	}{
		{naive{}, []float64{16, 16, 16}},
		{movingAverage{4}, []float64{12, 12, 12}}, // (11+13+8+16)/4
		{seasonalNaive{4}, []float64{11, 13, 8}},  // the same week one season ago
		{ses{0.5}, []float64{12.984375, 12.984375, 12.984375}},
	}
	for _, c := range cases {
		got := c.m.Forecast(x, 3)
		for i := range got {
			if !near(got[i], c.want[i]) {
				t.Errorf("%s = %v, want %v", c.m.Name(), got, c.want)
				break
			}
		}
	}
	// SES with alpha 1 is naive; a moving average of a constant is the constant.
	if f := (ses{1}).Forecast(x, 1); f[0] != 16 {
		t.Errorf("ses(1) = %v", f)
	}
	if f := (movingAverage{8}).Forecast(repeat(7, 8), 1); f[0] != 7 {
		t.Errorf("ma of constant = %v", f)
	}
}

// spy records the history it was given: the backtest must never show a
// method a value from on or after its forecast origin.
type spy struct {
	seen [][]float64
}

func (s *spy) Name() string    { return "spy" }
func (s *spy) Version() string { return "0" }
func (s *spy) MinHistory() int { return 1 }
func (s *spy) Forecast(x []float64, h int) []float64 {
	s.seen = append(s.seen, append([]float64(nil), x...))
	return repeat(0, h)
}

func TestBacktestingUsesTimeOrderedData(t *testing.T) {
	x := []float64{1, 2, 3, 4, 5, 6}
	sp := &spy{}
	sc := Backtest(sp, x, 3, 2)
	if sc.Folds != 3 || len(sp.seen) != 3 {
		t.Fatalf("folds = %d", sc.Folds)
	}
	for i, h := range sp.seen {
		if len(h) != 3+i {
			t.Fatalf("fold %d trained on %d points", i, len(h))
		}
		for j, v := range h {
			if v != x[j] {
				t.Fatalf("fold %d saw %v (not the past in order)", i, h)
			}
		}
	}
	// Points scored: origins 3,4,5 with horizon 2, cut at the end: 2+2+1.
	if len(sc.Residuals) != 5 {
		t.Errorf("residuals = %v", sc.Residuals)
	}
}

func TestEvaluationMetricsAreCorrect(t *testing.T) {
	// Naive on 10, 20, 10, 20 from origin 2, horizon 1:
	// origin 2 forecasts 20, actual 10 → error −10; origin 3 forecasts 10, actual 20 → +10.
	sc := Backtest(naive{}, []float64{10, 20, 10, 20}, 2, 1)
	if *sc.MAE != 10 || *sc.Bias != 0 || !near(*sc.WAPE, 20.0/30.0) {
		t.Errorf("mae %v bias %v wape %v", *sc.MAE, *sc.Bias, *sc.WAPE)
	}
	// MASE scale: training 10,20 → 10; training 10,20,10 → (10+10)/2 = 10. MAE 10 / 10 = 1.
	if sc.MASE == nil || !near(*sc.MASE, 1) {
		t.Errorf("mase = %v", sc.MASE)
	}
	// Bias sign: a method that always says 0 under-forecasts (negative bias).
	under := Backtest(movingAverage{1}, []float64{0, 5, 5}, 1, 1)
	if *under.Bias >= 0 {
		t.Errorf("bias of under-forecasting = %v", *under.Bias)
	}
	// Undefined, not zero: WAPE with no demand, MASE with a flat history.
	flat := Backtest(naive{}, []float64{0, 0, 0, 0}, 2, 1)
	if flat.WAPE != nil || flat.MASE != nil || *flat.MAE != 0 {
		t.Errorf("flat: wape %v mase %v", flat.WAPE, flat.MASE)
	}
	// Pooling across meals adds errors and actuals, never averages ratios.
	a := Backtest(naive{}, []float64{10, 20, 10, 20}, 2, 1)
	b := Backtest(naive{}, []float64{100, 100, 100, 100}, 2, 1)
	p := Combine("naive", []Score{a, b})
	if !near(*p.WAPE, 20.0/230.0) || *p.MAE != 5 {
		t.Errorf("pooled wape %v mae %v", *p.WAPE, *p.MAE)
	}
}

func TestSelectionKeepsNaiveUnlessBeaten(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	scores := []Score{{Method: "naive", MAE: f(10)}, {Method: "moving_average(4)", MAE: f(9.5)}, {Method: "ses(0.2)", MAE: f(8)}}
	if s, _ := Select(scores, 0.1); s.Method != "ses(0.2)" {
		t.Errorf("selected %s", s.Method)
	}
	if s, _ := Select(scores, 0.25); s.Method != "naive" {
		t.Errorf("selected %s with a 25%% bar", s.Method)
	}
	if _, ok := Select([]Score{{Method: "ses(0.2)", MAE: f(1)}}, 0); ok {
		t.Error("selected without a naive benchmark")
	}
	if _, _, ok := Interval([]float64{1, 2, 3}, 5); ok {
		t.Error("interval from too few residuals")
	}
	lo, hi, ok := Interval([]float64{-2, -1, 0, 1, 2, 3, 4, 5, 6, 7, 8}, 5)
	if !ok || !near(lo, -1) || !near(hi, 7) {
		t.Errorf("interval %v..%v", lo, hi)
	}
}
