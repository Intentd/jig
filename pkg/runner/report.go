package runner

import (
	"fmt"
	"strings"
	"time"

	"github.com/intentd/jig/pkg/expect"
)

// printReport prints the run's results: passed, failed with their errors,
// skipped, the post-block warnings, and the summary.
func (r *runner) printReport(result *expect.Result) {
	fmt.Printf("\n%s Results: %s\n\n", result.CallerName(), result.Name)
	var failed, skipped []expect.CaseResult
	for _, c := range result.Cases {
		switch {
		case c.Passed:
			fmt.Printf("  %s %-40s (%s)\n", successMark(), c.Name, c.Elapsed.Round(time.Millisecond))
		case c.Skipped:
			skipped = append(skipped, c)
		default:
			failed = append(failed, c)
		}
	}
	if len(failed) > 0 {
		fmt.Println()
		for _, c := range failed {
			fmt.Printf("  %s %-40s (%s)\n", failureMark(), c.Name, c.Elapsed.Round(time.Millisecond))
			if c.Err != nil {
				for _, line := range strings.Split(strings.TrimSpace(c.Err.Error()), "\n") {
					fmt.Printf("      %s\n", line)
				}
			}
		}
	}
	if len(skipped) > 0 {
		fmt.Println()
		for _, c := range skipped {
			fmt.Printf("  ~ %-40s (skipped)\n", c.Name)
		}
	}
	// Post-block warnings never change a result, so they are listed apart.
	warned := false
	for _, c := range result.Cases {
		for _, w := range c.Warnings {
			if !warned {
				fmt.Println("\n  Warnings (post blocks; results unchanged):")
				warned = true
			}
			fmt.Printf("    ! %s: %s\n", c.Name, strings.SplitN(w, "\n", 2)[0])
		}
	}
	fmt.Printf("\n  %s\n", result.Summary())
	if r.needsCluster {
		fmt.Printf("  Cluster: %s (%s)\n", r.clusterName(), r.spec.Environment.Provider())
	}
}
