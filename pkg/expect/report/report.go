// Package report renders jig results as Markdown, JSON and JUnit XML
// reports.
package report

import (
	"time"

	"github.com/intentd/jig/pkg/expect"
)

type caseReport struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Skipped  bool   `json:"skipped,omitempty"`
	Elapsed  string `json:"elapsed"`
	Error    string `json:"error,omitempty"`
	Attempts int    `json:"attempts,omitempty"` // attempts run; 0 when skipped
	Retries  int    `json:"retries,omitempty"`  // retries configured; omitted when none
	// Warnings are post-block failures (onFailure, finally); they never
	// change passed.
	Warnings []string `json:"warnings,omitempty"`
}

// setupReport is what stopped the run before its expectations finished: the
// environment or a prepare step.
type setupReport struct {
	At    string `json:"at"`
	Error string `json:"error"`
}

type stopReport struct {
	Reason  string `json:"reason"`
	After   int    `json:"after"`
	Ran     int    `json:"ran"`
	Planned int    `json:"planned"`
}

type resultReport struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type importReport struct {
	Path   string  `json:"path"`
	Result *report `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
}

type report struct {
	Name      string         `json:"name"`
	SpecPath  string         `json:"specPath,omitempty"`
	Context   string         `json:"context,omitempty"`
	Cluster   string         `json:"cluster,omitempty"`
	StartedAt *time.Time     `json:"startedAt,omitempty"`
	Command   string         `json:"command,omitempty"`
	Setup     *setupReport   `json:"setup,omitempty"`
	Stop      *stopReport    `json:"stop,omitempty"`
	Result    resultReport   `json:"result"`
	Elapsed   string         `json:"elapsed"`
	Warnings  []string       `json:"warnings,omitempty"` // the suite onFailure's failures
	Cases     []caseReport   `json:"cases,omitempty"`
	Imports   []importReport `json:"imports,omitempty"`
}

// resultWithImports returns counts for this suite and all imported tests.
func resultWithImports(r *expect.Result) resultReport {
	result := resultReport{
		Total:   r.Total(),
		Passed:  r.Passed(),
		Failed:  r.Failed(),
		Skipped: r.Skipped(),
	}

	if !r.WithImports() {
		return result
	}

	for _, imp := range r.Imports {
		if imp.Result == nil {
			continue
		}

		child := resultWithImports(imp.Result)
		result.Total += child.Total
		result.Passed += child.Passed
		result.Failed += child.Failed
		result.Skipped += child.Skipped
	}

	return result
}
