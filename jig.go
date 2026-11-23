// Package jig declares a Kubernetes environment and the checks that prove it
// correct. A jig is one YAML file: the cluster and its images and
// dependencies (environment), the steps that deploy and act on the workload
// (prepare), the checks (expect), and what to run on failure and at the end.
//
// A run has four phases: Environment, Prepare, Assert, Report. Expectations
// run after the environment, after a named prepare step, or after the last
// one.
//
//	decl, err := jig.FromFile("jig.yaml")
//	errs := jig.Validate(decl)
//	result, err := jig.Run(ctx, decl, jig.Options{})
package jig

import (
	"context"
	"io"
	"text/template"

	"github.com/intentd/jig/pkg/check"
	"github.com/intentd/jig/pkg/expect"
	"github.com/intentd/jig/pkg/expect/report"
	"github.com/intentd/jig/pkg/plan"
	"github.com/intentd/jig/pkg/render"
	"github.com/intentd/jig/pkg/runner"
	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/fileio"
)

type (
	// Declaration is a jig: a jig.yaml.
	Declaration = spec.Declaration
	// Options configure a run.
	Options = runner.Options
	// Result is a run's result, as the report shows it.
	Result = expect.Result
)

// FileName is the file jig reads in a directory, and finds in discovery.
const FileName = "jig.yaml"

// Discover returns every jig.yaml under root, sorted, as absolute paths,
// leaving out the ones that only import (a suite runs its imports, which
// discovery finds on their own) and those whose path matches a skip
// pattern.
func Discover(root string, skip []string) ([]string, error) {
	return fileio.DiscoverFiles(root, FileName, skip, spec.IsAggregator)
}

// Suite is a jig that imports paths in order, sharing one cluster, with
// wait between them (not before the first): what jig run ./... runs.
func Suite(name string, paths []string, wait string) *Declaration {
	d := &Declaration{Name: name}
	for i, p := range paths {
		imp := Import{Path: p}
		if i > 0 {
			imp.Wait = wait
		}
		d.Imports = append(d.Imports, imp)
	}
	return d
}

// FromFile reads and checks the jig at path: includes expanded,
// relative paths resolved from its directory. An error lists every problem.
func FromFile(path string) (*Declaration, error) {
	return check.FromFile(path)
}

// Plan builds a structured plan from a declaration without executing it.
func Plan(d *Declaration) (plan.Result, error) {
	return plan.Build(d)
}

// PlanAndReport builds and writes a declaration's plan in the requested format.
// Supported formats are "text" and "json". An empty format defaults to "text".
func PlanAndReport(w io.Writer, d *Declaration, format string) error {
	return plan.BuildAndReport(w, d, plan.Format(format))
}

// Render builds the artifacts needed to reproduce a Jig run.
func Render(d *spec.Declaration) (*spec.Render, error) {
	return render.Build(d)
}

// Validate returns every problem with s.
func Validate(d *Declaration) []error {
	return check.Validate(d)
}

// ValidateWith is Validate for a caller that runs with its own template
// functions (Options.Functions): templates are checked against funcs.
func ValidateWith(d *Declaration, funcs template.FuncMap) []error {
	return check.ValidateWith(d, funcs)
}

// Run runs d: the cluster, its images and environment, the prepare steps in
// order with the expectations after each, the imports, teardown, and the
// report. The error is set when the run could not complete or an
// expectation failed.
func Run(ctx context.Context, d *Declaration, o Options) (*Result, error) {
	return runner.Run(ctx, d, o)
}

// Write writes the result to the configured report file.
// The output format is determined by the file extension: JSON for .json,
// JUnit XML for .xml, Markdown otherwise.
func Report(ctx context.Context, file string, result expect.Result) error {
	return report.Write(ctx, file, &result)
}
