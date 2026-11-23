// Command runjig runs a jig file: load, validate, plan, run, and optionally
// write the report. It is the smallest program that uses jig as a library,
// and it shows the whole library surface in five calls.
//
//	go run ./runjig jig.yaml
//	go run ./runjig jig.yaml report.json
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/intentd/jig"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("a jig file is required: runjig <jig.yaml> [report]")
	}
	file := args[0]
	reportFile := ""
	if len(args) > 1 {
		reportFile = args[1]
	}

	stage("Load")
	decl, err := jig.FromFile(file)
	if err != nil {
		return fmt.Errorf("loading %s: %w", file, err)
	}
	fmt.Printf("  ✓ %s\n", file)

	stage("Validate")
	errs := jig.Validate(decl)
	if len(errs) > 0 {
		fmt.Printf("  ✗ %d problem(s)\n", len(errs))
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("invalid jig:\n  %s", strings.Join(msgs, "\n  "))
	}
	fmt.Println("  ✓ valid")

	stage("Plan")
	plan, err := jig.Plan(decl)
	if err != nil {
		return fmt.Errorf("planning %s: %w", file, err)
	}
	if err := plan.Write(os.Stdout, "json"); err != nil {
		return fmt.Errorf("writing plan %s: %w", file, err)
	}

	stage("Run")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := jig.Run(ctx, decl, jig.Options{Keep: true})
	if err != nil {
		return fmt.Errorf("running %s: %w", file, err)
	}
	if result != nil {
		fmt.Printf("\n  %d of %d passed\n", result.Passed(), result.Total())
	}

	stage("Report")
	if reportFile == "" {
		fmt.Println("  (none: pass a second argument to write one)")
		return nil
	}
	fmt.Print(" ✓")
	if err := jig.Report(ctx, reportFile, *result); err != nil {
		return fmt.Errorf("writing report %s: %w", reportFile, err)
	}
	return nil
}

// stage prints a banner for one of the four stages, so the output has
// visible boundaries between what the library did.
func stage(name string) {
	fmt.Printf("\n── %s %s\n", name, strings.Repeat("─", 10-len(name)))
}
