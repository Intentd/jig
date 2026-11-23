package render

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/intentd/jig/pkg/spec"
)

// renderApply renders the apply entries into files under the apply
// directory. Source files are copied as-is and formatted when written.
// Generated entries are skipped because their files are produced at run time.
func renderApply(entries spec.ApplyEntries, baseDir string) (spec.RenderFiles, error) {
	var files spec.RenderFiles

	for i, entry := range entries {
		if entry.Generated {
			continue
		}

		if entry.Path == "" {
			return nil, fmt.Errorf("apply entry %d: path is empty", i)
		}

		source := entry.Path
		if !filepath.IsAbs(source) {
			source = filepath.Join(baseDir, source)
		}

		data, err := os.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("read apply file %q: %w", entry.Path, err)
		}

		files = append(files, spec.RenderFile{
			Path:    filepath.Join("apply", entry.Path),
			Content: data,
			YAML:    true,
		})
	}

	return files, nil
}
