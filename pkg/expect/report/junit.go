package report

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/intentd/jig/pkg/expect"
)

// JUnit renders the result as JUnit XML, for CI systems that show test
// results natively: a suite for the test and one for each import (imports of
// imports included, flattened), a testcase per expectation. A failed
// expectation has a failure with its error; a skipped one is skipped. An
// import that produced no result (it could not be read or set up) is a suite
// with one failed "setup" testcase, and a test whose environment or a step
// failed has a failed "setup: <where>" testcase, so the CI shows why.
func JUnit(r *expect.Result) ([]byte, error) {
	out := junitSuites{Name: r.Name}
	addSuites(&out, r.Name, r, nil)
	for _, s := range out.Suites {
		out.Tests += s.Tests
		out.Failures += s.Failures
		out.Skipped += s.Skipped
	}
	out.Time = seconds(durationWithImports(r))
	data, err := xml.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding JUnit report: %w", err)
	}
	return append([]byte(xml.Header), append(data, '\n')...), nil
}

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name      string      `xml:"name,attr"`
	Tests     int         `xml:"tests,attr"`
	Failures  int         `xml:"failures,attr"`
	Skipped   int         `xml:"skipped,attr"`
	Time      string      `xml:"time,attr"`
	Timestamp string      `xml:"timestamp,attr,omitempty"`
	Cases     []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *struct{}     `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// addSuites adds r's suite, then its imports' suites in order. name is the
// suite's name: the jig's, or the import's path when it produced no result.
func addSuites(out *junitSuites, name string, r *expect.Result, importErr error) {
	if r == nil {
		out.Suites = append(out.Suites, junitSuite{
			Name: name, Tests: 1, Failures: 1, Time: seconds(0),
			Cases: []junitCase{{
				Name: "setup", Classname: name, Time: seconds(0),
				Failure: failure(importErr),
			}},
		})
		return
	}
	s := junitSuite{Name: name, Time: seconds(r.Elapsed)}
	if !r.StartedAt.IsZero() {
		s.Timestamp = r.StartedAt.UTC().Format(time.RFC3339)
	}
	for _, c := range r.Cases {
		jc := junitCase{Name: c.Name, Classname: name, Time: seconds(c.Elapsed)}
		switch {
		case c.Skipped:
			jc.Skipped = &struct{}{}
			s.Skipped++
		case !c.Passed:
			jc.Failure = failure(c.Err)
			s.Failures++
		}
		s.Cases = append(s.Cases, jc)
	}
	if f := r.SetupFailure; f != nil {
		s.Cases = append(s.Cases, junitCase{
			Name: "setup: " + f.At, Classname: name, Time: seconds(0),
			Failure: failure(errors.New(f.Err)),
		})
		s.Failures++
	}
	s.Tests = len(s.Cases)
	out.Suites = append(out.Suites, s)

	for _, imp := range r.Imports {
		child := imp.Path
		if imp.Result != nil && imp.Result.Name != "" {
			child = imp.Result.Name
		}
		addSuites(out, child, imp.Result, imp.Err)
	}
}

// failure is a JUnit failure for err: its first line as the message, all of
// it as the text.
func failure(err error) *junitFailure {
	if err == nil {
		return &junitFailure{Message: "failed"}
	}
	text := err.Error()
	msg, _, _ := strings.Cut(text, "\n")
	return &junitFailure{Message: msg, Text: text}
}

// durationWithImports is the run's time with its imports'.
func durationWithImports(r *expect.Result) time.Duration {
	d := r.Elapsed
	for _, imp := range r.Imports {
		if imp.Result != nil {
			d += durationWithImports(imp.Result)
		}
	}
	return d
}

func seconds(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}
