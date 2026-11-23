package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/intentd/jig/pkg/expect"
)

// Markdown renders the result as a GitHub-flavoured markdown summary block.
func Markdown(r *expect.Result) string {
	var b strings.Builder
	writeMarkdown(r, &b, 2)
	return b.String()
}

func writeMarkdown(r *expect.Result, b *strings.Builder, level int) {
	heading := strings.Repeat("#", level)

	var passed, failed, skipped []expect.CaseResult
	for _, c := range r.Cases {
		switch {
		case c.Skipped:
			skipped = append(skipped, c)
		case c.Passed:
			passed = append(passed, c)
		default:
			failed = append(failed, c)
		}
	}

	fmt.Fprintf(b, "%s %s Results: %s\n\n", heading, r.CallerName(), r.Name)

	b.WriteString("| Property | Value |\n|---|---|\n")
	if r.SpecPath != "" {
		fmt.Fprintf(b, "| Spec | %s |\n", r.SpecPath)
	}

	if r.Context != "" {
		fmt.Fprintf(b, "| Cluster context | %s |\n", r.Context)
	}
	if r.Cluster != "" {
		fmt.Fprintf(b, "| Cluster | %s |\n", r.Cluster)
	}
	if !r.StartedAt.IsZero() {
		fmt.Fprintf(b, "| Started | %s |\n", r.StartedAt.Format(time.RFC3339))
	}

	if r.WithImports() {
		fmt.Fprintf(b, "| Duration | %s |\n\n", r.DurationWithImports())
	} else {
		fmt.Fprintf(b, "| Duration | %s |\n\n", r.Duration())
	}

	// Results for this suite and all imported tests.
	result := resultWithImports(r)

	b.WriteString("| Result | |\n|---|---|\n")
	fmt.Fprintf(b, "| Total | %d |\n", result.Total)
	fmt.Fprintf(b, "| ✅ Passed | %d |\n", result.Passed)
	fmt.Fprintf(b, "| ❌ Failed | %d |\n", result.Failed)
	fmt.Fprintf(b, "| ⏭ Skipped | %d |\n\n", result.Skipped)

	if f := r.SetupFailure; f != nil {
		msg, _, _ := strings.Cut(f.Err, "\n")
		fmt.Fprintf(b, "> ❌ **Setup failed** at `%s`: %s — %d of %d expectations run\n\n", f.At, msg, r.Total(), r.Planned)
	}

	if r.StopReason != "" {
		failureStr := "failure"
		if r.StoppedAfter != 1 {
			failureStr = "failures"
		}

		fmt.Fprintf(b,
			"> ⛔ **Stopped** after %d %s (%s) — %d of %d expectations run\n\n",
			r.StoppedAfter,
			failureStr,
			r.StopReason,
			r.Total(),
			r.Planned,
		)
	}

	if retried := r.Retried(); retried > 0 {
		fmt.Fprintf(b,
			"> 🔁 **%d expectation(s) needed a retry** — %d extra attempt(s) total\n\n",
			retried,
			r.ExtraAttempts(),
		)
	}

	if r.Command != "" {
		b.WriteString("**Command:**\n\n```bash\n")
		b.WriteString(r.Command)
		b.WriteString("\n```\n\n")
	}

	// Only add the Attempts column when at least one case retried; otherwise
	// the table shape is unchanged from a suite with no retries.
	anyRetried := r.Retried() > 0

	writeCases := func(title string, cases []expect.CaseResult, withError bool) {
		if len(cases) == 0 {
			return
		}

		fmt.Fprintf(b, "%s %s\n\n", strings.Repeat("#", level+1), title)

		switch {
		case withError && anyRetried:
			b.WriteString("| Test | Attempts | Time | Error |\n|---|---|---|---|\n")
			for _, c := range cases {
				msg := ""
				if c.Err != nil {
					msg = strings.ReplaceAll(strings.TrimSpace(c.Err.Error()), "\n", " ")
				}
				fmt.Fprintf(b, "| ❌ %s | %s | %s | %s |\n",
					c.Name, expect.AttemptsCell(c), c.Elapsed.Round(time.Millisecond), msg)
			}

		case withError:
			b.WriteString("| Test | Time | Error |\n|---|---|---|\n")
			for _, c := range cases {
				msg := ""
				if c.Err != nil {
					msg = strings.ReplaceAll(strings.TrimSpace(c.Err.Error()), "\n", " ")
				}
				fmt.Fprintf(b, "| ❌ %s | %s | %s |\n",
					c.Name, c.Elapsed.Round(time.Millisecond), msg)
			}

		case anyRetried:
			b.WriteString("| Test | Attempts | Time |\n|---|---|---|\n")
			for _, c := range cases {
				mark := "✅"
				if c.Skipped {
					mark = "⏭"
				}
				fmt.Fprintf(b, "| %s %s | %s | %s |\n",
					mark, c.Name, expect.AttemptsCell(c), c.Elapsed.Round(time.Millisecond))
			}

		default:
			b.WriteString("| Test | Time |\n|---|---|\n")
			for _, c := range cases {
				mark := "✅"
				if c.Skipped {
					mark = "⏭"
				}
				fmt.Fprintf(b, "| %s %s | %s |\n",
					mark, c.Name, c.Elapsed.Round(time.Millisecond))
			}
		}

		b.WriteString("\n")
	}

	writeCases("Failed", failed, true)
	writeCases("Passed", passed, false)
	writeCases("Skipped", skipped, false)
	writeWarnings(b, r, level)

	if r.WithImports() {
		fmt.Fprintf(b, "%s Imported %s Suites\n\n",
			strings.Repeat("#", level+1), r.CallerName())

		for _, imp := range r.Imports {
			if imp.Result != nil {
				writeMarkdown(imp.Result, b, level+2)
			} else {
				fmt.Fprintf(b, "%s %s\n\n",
					strings.Repeat("#", level+2),
					imp.Name(),
				)
			}

			if imp.Err != nil {
				b.WriteString("❌ **Import failed:** ")
				b.WriteString(strings.ReplaceAll(imp.Err.Error(), "\n", " "))
				b.WriteString("\n\n")
			}
		}
	}

	fmt.Fprintf(b, "**%s**\n\n", summaryWithImports(r))
}

// summaryWithImports is the closing line: the same counts as the Result
// table above it, imports included, and the time with imports.
func summaryWithImports(r *expect.Result) string {
	res := resultWithImports(r)
	s := fmt.Sprintf("%d of %d passed", res.Passed, res.Total)
	if res.Skipped > 0 {
		s += fmt.Sprintf(", %d skipped", res.Skipped)
	}
	if f := r.SetupFailure; f != nil {
		s += fmt.Sprintf(", setup failed at %s", f.At)
	}
	if n := failedImports(r); n > 0 {
		s += fmt.Sprintf(", %d import(s) failed", n)
	}
	d := r.Duration()
	if r.WithImports() {
		d = r.DurationWithImports()
	}
	return s + fmt.Sprintf(" (%s)", d)
}

// writeWarnings lists the post-block warnings — each case's onFailure and
// finally, then the suite onFailure — which never change a result.
func writeWarnings(b *strings.Builder, r *expect.Result, level int) {
	var lines []string
	for _, c := range r.Cases {
		for _, w := range c.Warnings {
			lines = append(lines, fmt.Sprintf("- **%s**: %s", c.Name, oneLine(w)))
		}
	}
	for _, w := range r.Warnings {
		lines = append(lines, "- **suite**: "+oneLine(w))
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "%s Warnings\n\nPost-block failures (onFailure, finally); they do not change the results.\n\n", strings.Repeat("#", level+1))
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n\n")
}

func oneLine(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
}

// failedImports counts r's imports that failed, at any depth.
func failedImports(r *expect.Result) int {
	n := 0
	for _, imp := range r.Imports {
		if imp.Err != nil {
			n++
		}
		if imp.Result != nil {
			n += failedImports(imp.Result)
		}
	}
	return n
}
