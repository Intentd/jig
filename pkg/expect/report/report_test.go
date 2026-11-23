package report

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/jig/pkg/expect"
	"github.com/intentd/tools/structured/structuredtest"
)

// update rewrites the golden files: go test ./expect/report -update.
var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// sample is one result with every kind of outcome: passed, failed, skipped
// and retried expectations, a warning, a stop, an import that ran, one whose
// step failed, and one that could not be read.
func sample() *expect.Result {
	return &expect.Result{
		Caller:       "jig",
		Name:         "shop",
		SpecPath:     "e2e/jig.yaml",
		Context:      "kind-shop",
		Cluster:      "shop (kind)",
		Command:      "jig run e2e --report",
		StartedAt:    time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC),
		Elapsed:      3 * time.Second,
		Planned:      4,
		StopReason:   "step",
		StoppedAfter: 1,
		Warnings:     []string{"onFailure: kubectl logs: no pods match app=shop"},
		Cases: []expect.CaseResult{
			{Name: "the API is up", Passed: true, Elapsed: 1500 * time.Millisecond, Attempts: 2, Retries: 3},
			{Name: "the order is accepted", Err: errors.New("exit code 1, want 0\ncurl: (7) refused"), Elapsed: time.Second, Attempts: 1},
			{Name: "only on prod", Skipped: true},
		},
		Imports: expect.ImportResults{
			{Path: "./upgrade/jig.yaml", Result: &expect.Result{
				Name:     "upgrade",
				SpecPath: "./upgrade/jig.yaml",
				Elapsed:  2 * time.Second,
				Planned:  1,
				Cases:    []expect.CaseResult{{Name: "upgraded", Passed: true, Elapsed: 2 * time.Second, Attempts: 1}},
			}},
			{Path: "./db/jig.yaml", Err: errors.New("migrate: apply ./job.yaml: exit status 1"), Result: &expect.Result{
				Name:         "db",
				SpecPath:     "./db/jig.yaml",
				Elapsed:      time.Second,
				Planned:      2,
				Cases:        []expect.CaseResult{{Name: "postgres is up", Passed: true, Elapsed: time.Second, Attempts: 1}},
				SetupFailure: &expect.SetupFailure{At: "migrate", Err: "migrate: apply ./job.yaml: exit status 1\nerror: no objects passed to apply"},
			}},
			{Path: "./broken/jig.yaml", Err: errors.New("loading import ./broken/jig.yaml: no such file")},
		},
	}
}

// golden compares got with testdata/name, or writes it there with -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		structuredtest.WriteFile(t, path, string(got))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden file: go test ./expect/report -update")
	assert.Equal(t, string(want), string(got))
}

func TestMarkdown(t *testing.T) {
	golden(t, "report.md", []byte(Markdown(sample())))
}

func TestJSON(t *testing.T) {
	got, err := JSON(sample())
	require.NoError(t, err)
	golden(t, "report.json", got)
}

func TestJUnit(t *testing.T) {
	got, err := JUnit(sample())
	require.NoError(t, err)
	golden(t, "report.xml", got)
}

// Write picks the format by the extension, and writes nothing without a
// file.
func TestWrite(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, Write(ctx, "", sample()))
	dir := t.TempDir()
	docs := map[string]string{"r.md": "report.md", "r.json": "report.json", "r.xml": "report.xml"}
	for name, want := range docs {
		path := filepath.Join(dir, name)
		require.NoError(t, Write(ctx, path, sample()))
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		golden(t, want, got)
	}
}
