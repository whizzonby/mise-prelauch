// Package forecasting is Mise's demand forecasting (Sprint 8): baseline
// statistical methods, rolling-origin backtests and a readiness gate. It is
// advisory only. It never writes production plans, procurement plans or
// purchase orders, and it refuses to forecast from test or demo history.
package forecasting

import (
	"fmt"
	"math"
	"sort"
)

// Method forecasts the next h values from a history ordered oldest first.
// Implementations are pure and deterministic.
type Method interface {
	Name() string
	Version() string
	Forecast(history []float64, h int) []float64
	MinHistory() int
}

type naive struct{}

func (naive) Name() string    { return "naive" }
func (naive) Version() string { return "1" }
func (naive) MinHistory() int { return 1 }
func (naive) Forecast(x []float64, h int) []float64 {
	return repeat(x[len(x)-1], h)
}

type seasonalNaive struct{ s int }

func (m seasonalNaive) Name() string    { return fmt.Sprintf("seasonal_naive(%d)", m.s) }
func (m seasonalNaive) Version() string { return "1" }
func (m seasonalNaive) MinHistory() int { return m.s }
func (m seasonalNaive) Forecast(x []float64, h int) []float64 {
	out := make([]float64, h)
	for i := range out {
		out[i] = x[len(x)-m.s+i%m.s]
	}
	return out
}

type movingAverage struct{ k int }

func (m movingAverage) Name() string    { return fmt.Sprintf("moving_average(%d)", m.k) }
func (m movingAverage) Version() string { return "1" }
func (m movingAverage) MinHistory() int { return m.k }
func (m movingAverage) Forecast(x []float64, h int) []float64 {
	sum := 0.0
	for _, v := range x[len(x)-m.k:] {
		sum += v
	}
	return repeat(sum/float64(m.k), h)
}

// ses is simple exponential smoothing with a fixed alpha, initialised on the
// first observation.
type ses struct{ alpha float64 }

func (m ses) Name() string    { return fmt.Sprintf("ses(%.1f)", m.alpha) }
func (m ses) Version() string { return "1" }
func (m ses) MinHistory() int { return 2 }
func (m ses) Forecast(x []float64, h int) []float64 {
	level := x[0]
	for _, v := range x[1:] {
		level = m.alpha*v + (1-m.alpha)*level
	}
	return repeat(level, h)
}

func repeat(v float64, h int) []float64 {
	out := make([]float64, h)
	for i := range out {
		out[i] = v
	}
	return out
}

// Candidates are the baseline methods tried. Seasonal naive is included only
// when a season length is configured.
func Candidates(season int) []Method {
	out := []Method{naive{}, movingAverage{4}, movingAverage{8}, ses{0.2}, ses{0.5}}
	if season >= 2 {
		out = append(out, seasonalNaive{season})
	}
	return out
}

// Naive is the benchmark every candidate must beat.
func Naive() Method { return naive{} }

// ---------------------------------------------------------------- backtest

// Score is a method's out-of-sample accuracy. A nil field is undefined
// (WAPE when actual demand is zero, MASE when the naive in-sample error is
// zero), never reported as zero.
type Score struct {
	Method    string    `json:"method"`
	Folds     int       `json:"folds"`
	MAE       *float64  `json:"mae"`
	WAPE      *float64  `json:"wape"`
	Bias      *float64  `json:"bias"`
	MASE      *float64  `json:"mase"`
	Residuals []float64 `json:"-"` // actual − forecast, one per scored point
	absErr    float64
	actual    float64
	scaled    float64 // sum of in-sample naive absolute errors over folds
	scaledN   int
}

// Backtest scores a method by rolling origin: for each origin from minTrain
// to the end, fit on everything before it and forecast h steps; only points
// that have happened are scored. Strictly time-ordered: no future value is
// ever seen when fitting.
func Backtest(m Method, x []float64, minTrain, h int) Score {
	sc := Score{Method: m.Name()}
	if minTrain < m.MinHistory() {
		minTrain = m.MinHistory()
	}
	for origin := minTrain; origin < len(x); origin++ {
		train := x[:origin]
		f := m.Forecast(train, h)
		for i := 0; i < h && origin+i < len(x); i++ {
			e := x[origin+i] - f[i]
			sc.Residuals = append(sc.Residuals, e)
			sc.absErr += math.Abs(e)
			sc.actual += x[origin+i]
		}
		// MASE scale: mean absolute one-step naive error inside the training window.
		if len(train) >= 2 {
			s := 0.0
			for j := 1; j < len(train); j++ {
				s += math.Abs(train[j] - train[j-1])
			}
			sc.scaled += s / float64(len(train)-1)
			sc.scaledN++
		}
		sc.Folds++
	}
	finish(&sc)
	return sc
}

func finish(sc *Score) {
	n := float64(len(sc.Residuals))
	if n == 0 {
		return
	}
	mae := sc.absErr / n
	sum := 0.0
	for _, e := range sc.Residuals {
		sum += e
	}
	bias := -sum / n // positive: the method over-forecasts
	sc.MAE, sc.Bias = &mae, &bias
	if sc.actual > 0 {
		w := sc.absErr / sc.actual
		sc.WAPE = &w
	}
	if sc.scaledN > 0 && sc.scaled > 0 {
		m := mae / (sc.scaled / float64(sc.scaledN))
		sc.MASE = &m
	}
}

// Combine pools scores of one method across several series (meals).
func Combine(method string, parts []Score) Score {
	out := Score{Method: method}
	for _, p := range parts {
		out.Folds += p.Folds
		out.Residuals = append(out.Residuals, p.Residuals...)
		out.absErr += p.absErr
		out.actual += p.actual
		out.scaled += p.scaled
		out.scaledN += p.scaledN
	}
	finish(&out)
	return out
}

// Select picks the method with the lowest pooled MAE, but only if it beats
// naive by at least minImprovement (a share, e.g. 0.1 for 10%). Otherwise
// naive is kept: a more complex method must earn its place.
func Select(scores []Score, minImprovement float64) (Score, bool) {
	var base *Score
	for i := range scores {
		if scores[i].Method == Naive().Name() {
			base = &scores[i]
		}
	}
	if base == nil || base.MAE == nil {
		return Score{}, false
	}
	best := *base
	for _, s := range scores {
		if s.MAE != nil && *s.MAE < *best.MAE && *s.MAE <= *base.MAE*(1-minImprovement) {
			best = s
		}
	}
	return best, true
}

// Interval is an empirical prediction interval from backtest residuals: the
// 10th and 90th percentiles added to the point forecast. It is not
// estimated when there are fewer residuals than required.
func Interval(residuals []float64, min int) (lo, hi float64, ok bool) {
	if len(residuals) < min || len(residuals) == 0 {
		return 0, 0, false
	}
	r := append([]float64(nil), residuals...)
	sort.Float64s(r)
	return quantile(r, 0.1), quantile(r, 0.9), true
}

func quantile(sorted []float64, p float64) float64 {
	pos := p * float64(len(sorted)-1)
	i := int(math.Floor(pos))
	if i+1 >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	return sorted[i] + (pos-float64(i))*(sorted[i+1]-sorted[i])
}
