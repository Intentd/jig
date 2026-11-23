package plan

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/term"
)

type (
	declaration = spec.Declaration
	ecosystem   = spec.Ecosystem
	Step        = spec.Step
	After       = spec.After
)

var (
	afterPrepare        = spec.AfterPrepare
	errDeclarationEmpty = spec.ErrDeclarationEmpty
	modeCluster         = spec.ModeCluster
)

// Build builds a plan from a declaration.
// It describes what a run of d does, in order: the cluster, environment and
// images, prepare steps, imports, expectations and when they run, and
// environment variables that nothing sets. It resolves the same values as a
// run, using placeholders where necessary, and returns an error if a value
// cannot be resolved.
func Build(d *declaration) (Result, error) {
	return buildPlan(d)
}

// BuildAndReport builds a plan from a declaration and writes it in the
// requested format.
func BuildAndReport(w io.Writer, d *declaration, format Format) error {
	result, err := Build(d)
	if err != nil {
		return err
	}
	return result.Write(w, format.String())
}

// Write renders the plan in the requested format.
func (r Result) Write(w io.Writer, format string) error {
	f := Format(format)
	if f == "" {
		f = "text"
	}

	if !f.IsValid() {
		return fmt.Errorf("unsupported plan format %q", format)
	}

	switch f {
	case FormatText:
		return r.Text(w)
	case FormatJSON:
		return r.JSON(w)
	default:
		return fmt.Errorf("unsupported plan format %q", format)
	}
}

// WriteToFile writes the plan to a file in the requested format.
func (r Result) WriteToFile(path string, format string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create plan file %q: %w", path, err)
	}

	if err := r.Write(f, format); err != nil {
		_ = f.Close()
		return err
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close plan file %q: %w", path, err)
	}

	return nil
}

// JSON writes the machine-readable plan.
func (r Result) JSON(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)

	if err := encoder.Encode(r); err != nil {
		return fmt.Errorf("write plan JSON: %w", err)
	}

	return nil
}

// Text writes a human-readable representation of the plan.
func (r Result) Text(w io.Writer) error {
	gray := func(format string, a ...any) {
		fmt.Fprintf(w, "    %s\n", term.Gray(fmt.Sprintf(format, a...)))
	}

	fmt.Fprintf(w, "%s %s\n",
		term.HealthIcon("ready"),
		term.Bold(r.Name),
	)

	if r.Description != "" {
		gray("%s", r.Description)
	}

	switch r.Mode {
	case "commands":
		gray("mode    : no cluster (provider: none — commands only)")
	default:
		gray("cluster : kind %q", r.Cluster)
	}

	if e := r.Environment; e != nil {
		if len(e.Apply) > 0 {
			gray("environment.apply : %s", strings.Join(e.Apply, ", "))
		}
		if e.Helm > 0 {
			gray("environment.helm  : %d chart(s)", e.Helm)
		}
		if e.Wait > 0 {
			gray("environment.wait  : %d resource(s)", e.Wait)
		}

		printImagePlans(w, e.Images)
		printEcosystemPlan(w, e.Ecosystem)
	}

	if len(r.Prepare) > 0 {
		gray("prepare :")
		for _, step := range r.Prepare {
			fmt.Fprintf(w, "      [%d] %s %s\n",
				step.Index,
				step.Name,
				term.Gray("("+step.Action+")"),
			)
		}
	}

	if len(r.Imports) > 0 {
		gray("imports : %d file(s)", len(r.Imports))

		for _, imp := range r.Imports {
			label := imp.Path
			if imp.FreshCluster {
				label += " (fresh cluster)"
			}
			if imp.Wait != "" {
				label += " (wait: " + imp.Wait + ")"
			}

			fmt.Fprintf(w, "      %s %s\n",
				term.HealthIcon("ready"),
				term.Gray(label),
			)

			children := len(imp.Prepare) + len(imp.Expect)
			childIndex := 0

			for _, step := range imp.Prepare {
				childIndex++
				prefix := "          ├─"
				if childIndex == children {
					prefix = "          └─"
				}

				fmt.Fprintf(w, "%s [%d] %s %s\n",
					prefix,
					step.Index,
					step.Name,
					term.Gray("("+step.Action+")"),
				)
			}

			for _, expectation := range imp.Expect {
				childIndex++
				prefix := "          ├─"
				if childIndex == children {
					prefix = "          └─"
				}

				fmt.Fprintf(w, "%s %s\n",
					prefix,
					term.Gray(fmt.Sprintf("• %s (after: %s, timeout: %s)",
						expectation.Name,
						expectation.After,
						expectation.Timeout,
					)),
				)
			}
		}
	}

	if len(r.Expect) > 0 {
		fmt.Fprintln(w)
		for _, expectation := range r.Expect {
			gray("%-40s after: %-12s timeout: %s",
				expectation.Name,
				expectation.After,
				expectation.Timeout,
			)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, strings.Repeat("─", 60))

	if r.Summary.Aggregator {
		fmt.Fprintf(w, "%d import(s) valid\n", r.Summary.Imports)
	} else {
		fmt.Fprintf(w, "%d step(s), %d expectation(s) valid",
			r.Summary.Prepare,
			r.Summary.Expectations,
		)
		if r.Summary.Imports > 0 {
			fmt.Fprintf(w, ", %d import(s) valid", r.Summary.Imports)
		}
		fmt.Fprintln(w)
	}

	for _, warning := range r.Warnings {
		fmt.Fprintf(w, "\n%s  %s\n",
			term.Yellow("⚠"),
			warning.Message,
		)
	}

	if len(r.Guards) > 0 {
		gray("guards (at the end of the run):")
		for i, guard := range r.Guards {
			fmt.Fprintf(w, "      [%d] %s\n", i, guard)
		}
	}

	return nil
}
