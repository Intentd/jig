package jig_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/intentd/jig"
)

// TestExamples reads and checks every example, and runs the ones that need
// no cluster. The others run with `jig run examples/<name>`.
func TestExamples(t *testing.T) {
	paths, err := filepath.Glob("examples/*/jig.yaml")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	paths = append(paths, "examples/jig.yaml") // the suite
	for _, p := range paths {
		t.Run(filepath.Dir(p), func(t *testing.T) {
			test, err := jig.FromFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if test.Environment.Provider() != jig.ProviderNone {
				return
			}
			if _, err := jig.Run(context.Background(), test, jig.Options{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
