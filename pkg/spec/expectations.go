package spec

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// Expectations is a suite's expect list, or an include file's.
type Expectations []Expectation

// Names returns each expectation's name, in order.
func (e Expectations) Names() []string {
	out := make([]string, len(e))
	for i, exp := range e {
		out[i] = exp.Name
	}
	return out
}

// ExpandIncludes replaces each include: entry with the expectations its file
// lists, run from that file's directory, and each external include with its
// calls. Relative paths resolve against baseDir. An include file can include
// others, its paths resolved from its own directory; a file that includes
// itself, directly or through others, is an error naming the chain.
func (e Expectations) ExpandIncludes(baseDir string) (Expectations, error) {
	result, err := e.expand(baseDir, nil)
	if err != nil {
		return nil, err
	}
	for i := range result {
		external, err := expandExternalIncludes(result[i].External, result[i].Dir(baseDir))
		if err != nil {
			return nil, fmt.Errorf("expect %q: %w", result[i].Name, err)
		}
		result[i].External = external
	}
	return result, nil
}

// expand expands the include: entries of e, resolved against dir. chain is
// the include files being expanded, outermost first.
func (e Expectations) expand(dir string, chain []string) (Expectations, error) {
	var result Expectations
	for _, exp := range e {
		if exp.Include == "" {
			result = append(result, exp)
			continue
		}
		path := exp.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", exp.Include, err)
		}
		if slices.Contains(chain, abs) {
			return nil, fmt.Errorf("include %s: includes itself: %s", exp.Include, strings.Join(append(slices.Clone(chain), abs), " → "))
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", exp.Include, err)
		}
		var file ExpectInclude
		if err := strictUnmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("include %s: %w", exp.Include, err)
		}
		includeDir := filepath.Dir(path)
		nested, err := file.Expect.expand(includeDir, append(slices.Clone(chain), abs))
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", exp.Include, err)
		}
		// An expectation runs from the directory of the file that holds it;
		// those from deeper files already have theirs.
		for i := range nested {
			if nested[i].WorkDir == "" {
				nested[i].WorkDir = includeDir
			}
		}
		result = append(result, nested...)
	}
	return result, nil
}

// expandExternalIncludes replaces each include: entry in checks with the
// calls its file lists, resolved against dir.
func expandExternalIncludes(checks ExternalChecks, dir string) (ExternalChecks, error) {
	var out ExternalChecks
	for _, c := range checks {
		if c.Include == "" {
			out = append(out, c)
			continue
		}
		path := c.Include
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		data, err := readLocal(path)
		if err != nil {
			return nil, fmt.Errorf("reading external include %q: %w", c.Include, err)
		}
		var f ExternalInclude
		if err := strictUnmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parsing external include %q: %w", c.Include, err)
		}
		out = append(out, f.Calls...)
	}
	return out, nil
}
