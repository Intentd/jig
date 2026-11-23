package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/intentd/jig"
	"github.com/intentd/tools/term"
)

// discoveryFlags are the flags of ./... discovery.
type discoveryFlags struct {
	skip  []string
	wait  string
	list  bool
	limit int
}

func (d *discoveryFlags) add(cmd *cobra.Command, run bool) {
	f := cmd.Flags()
	f.StringSliceVar(&d.skip, "skip", nil, "with ./...: path patterns to leave out (repeatable or comma-separated, e.g. vendor,testdata)")
	if run {
		f.StringVar(&d.wait, "wait", "", "with ./...: pause between the discovered tests (e.g. 2s)")
		f.BoolVarP(&d.list, "list", "l", false, "with ./...: list the tests a run would find, without running them")
		f.IntVar(&d.limit, "limit", 10, "with --list: how many to show (0: all)")
	}
}

// discover finds the tests under root and prints them relative to it. It
// returns no paths, and says so, when there are none.
func discover(w io.Writer, root string, skip []string) ([]string, error) {
	paths, err := jig.Discover(root, skip)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		fmt.Fprintf(w, "No %s under %s\n", jig.FileName, root)
	}
	return paths, nil
}

// rel names path relative to root, for output.
func rel(root, path string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return path
	}
	if r, err := filepath.Rel(abs, path); err == nil {
		return r
	}
	return path
}

// listDiscovered prints the tests a run under root would find.
func listDiscovered(w io.Writer, root string, paths []string, limit int) {
	fmt.Fprintf(w, "→ Would run %d test(s) under %s\n\n", len(paths), root)
	shown := len(paths)
	if limit > 0 && limit < shown {
		shown = limit
	}
	for _, p := range paths[:shown] {
		fmt.Fprintf(w, "  %s\n", rel(root, p))
	}
	if shown < len(paths) {
		fmt.Fprintf(w, "  ... %d more\n", len(paths)-shown)
	}
}

// validateDiscovered checks every test under root and prints one line each.
func validateDiscovered(w io.Writer, root string, paths []string) error {
	fmt.Fprintf(w, "→ Validating %d test(s) under %s\n\n", len(paths), root)
	failed := 0
	for _, p := range paths {
		if _, err := jig.FromFile(p); err != nil {
			failed++
			fmt.Fprintf(w, "  %s %s\n", term.FailureMark(), rel(root, p))
			for _, line := range strings.Split(strings.TrimSpace(err.Error()), "\n") {
				fmt.Fprintf(w, "      %s\n", line)
			}
			continue
		}
		fmt.Fprintf(w, "  %s %s\n", term.SuccessMark(), rel(root, p))
	}
	fmt.Fprintln(w)
	if failed > 0 {
		return fmt.Errorf("%d of %d test(s) failed validation", failed, len(paths))
	}
	fmt.Fprintf(w, "%d test(s) valid\n", len(paths))
	return nil
}
