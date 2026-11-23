package expect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/structured/structuredtest"
)

// A repeated call's summary: count, errors, and durations by quantile.
func TestSummarize(t *testing.T) {
	ms := []float64{}
	for i := 1; i <= 100; i++ {
		ms = append(ms, float64(i))
	}
	s := summarize(ms, 2)
	assert.Equal(t, repeatSummary{Count: 100, Errors: 2, P50: 50, P95: 95, P99: 99, Max: 100, Mean: 50.5}, s)
	assert.Equal(t, repeatSummary{}, summarize(nil, 0))
}

// stays: the checks must go on passing for the window; the first failure
// in it fails the expectation, naming how long they held.
func TestHolds(t *testing.T) {
	old := pollInterval
	pollInterval = 50 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })

	dir := t.TempDir()
	flag := filepath.Join(dir, "up")
	structuredtest.WriteFile(t, flag, "")
	exp := spec.Expectation{Name: "up", Stays: "300ms", Commands: spec.Commands{{Run: "test -f up"}}}
	sc := spec.ExpectationScope{Expectation: exp, WorkDir: dir}
	require.NoError(t, holds(context.Background(), exp, stepOperations(exp), sc, nil))

	go func() {
		time.Sleep(120 * time.Millisecond)
		os.Remove(flag)
	}()
	err := holds(context.Background(), exp, stepOperations(exp), sc, nil)
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "held for "), err.Error())

	assert.NoError(t, holds(context.Background(), spec.Expectation{}, operations{}, sc, nil), "no stays: nothing to hold")
}

// capture: values saved by one expectation reach the ones after it.
func TestRunner_Captures(t *testing.T) {
	r := NewRunner(Options{Env: map[string]string{"A": "1"}})
	r.capture(map[string]string{"UID": "abc"})
	assert.Equal(t, map[string]string{"A": "1", "UID": "abc"}, r.withCaptured(map[string]string{"A": "1"}))
}
