package expect

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/assert"
	"github.com/intentd/tools/resolver"
)

// repeatSummary is what a repeated call's assertions read: how many calls,
// how many failed, and their durations in milliseconds.
type repeatSummary struct {
	Count  int     `json:"count"`
	Errors int     `json:"errors"`
	P50    float64 `json:"p50"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
}

// runExternalRepeat makes the call c.Repeat times and asserts on the
// summary: a budget ({path: .p95, lessThan: "200"}, or
// expr: "${ value.errors == 0 }"), not a load test. A call that fails, or
// answers an HTTP status of 400 or more, counts as an error.
func runExternalRepeat(ctx context.Context, c spec.ExternalCheck, r *resolver.Resolver, cs kubernetes.Interface) (string, error) {
	var ms []float64
	errs := 0
	for range c.Repeat {
		start := time.Now()
		result, err := spec.CallExternal(ctx, "external", r, c.ExternalCall, cs)
		ms = append(ms, float64(time.Since(start).Microseconds())/1000)
		if err != nil || result["error"] != nil && result["error"] != "" || httpFailed(result["status"]) {
			errs++
		}
	}
	sum := summarize(ms, errs)
	b, err := json.Marshal(sum)
	if err != nil {
		return "", err
	}
	out := string(b)
	checks := []assert.Assertions{{TextAssertion: c.TextAssertion}}
	if c.Assert != nil {
		checks = append(checks, *c.Assert)
	}
	for _, a := range checks {
		if a.Empty() {
			continue
		}
		if err := a.Check(out); err != nil {
			return out, fmt.Errorf("external %q ×%d: %w\nsummary: %s", c.Name, c.Repeat, err, out)
		}
	}
	return out, nil
}

// httpFailed reports whether an HTTP status is 400 or more.
func httpFailed(status any) bool {
	var n int
	if _, err := fmt.Sscan(fmt.Sprint(status), &n); err != nil {
		return false
	}
	return n >= 400
}

// summarize is the summary of durations ms with errs failed calls.
func summarize(ms []float64, errs int) repeatSummary {
	s := repeatSummary{Count: len(ms), Errors: errs}
	if len(ms) == 0 {
		return s
	}
	sorted := append([]float64(nil), ms...)
	sort.Float64s(sorted)
	at := func(q float64) float64 {
		i := int(math.Ceil(q*float64(len(sorted)))) - 1
		return sorted[max(0, min(i, len(sorted)-1))]
	}
	total := 0.0
	for _, v := range sorted {
		total += v
	}
	s.P50, s.P95, s.P99, s.Max = at(0.50), at(0.95), at(0.99), sorted[len(sorted)-1]
	s.Mean = math.Round(total/float64(len(sorted))*100) / 100
	return s
}
