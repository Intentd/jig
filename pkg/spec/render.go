package spec

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/intentd/tools/structured"
)

// Render represents the complete set of artifacts needed to reproduce a Jig run.
// Each phase contains the files required to perform that part of the run.
type Render struct {
	Phases []RenderPhase
}

// RenderPhase represents one ordered stage of a rendered run.
// Its name is used to create the phase directory.
type RenderPhase struct {
	Name  string
	Files []RenderFile
}

// RenderFile represents a file in a rendered phase.
// Path is relative to the phase directory.
type RenderFile struct {
	Path    string
	Content []byte
	YAML    bool
}

// RenderFiles are files in a rendered phase.
type RenderFiles []RenderFile

// Write writes the rendered files to disk, creating the numbered phase
// directories as needed. YAML files are formatted.
//
// When dryRun is true, the output paths are printed without writing files.
func (r *Render) Write(root string, dryRun bool) error {
	if r == nil {
		return fmt.Errorf("write render: render is nil")
	}

	if len(r.Phases) == 0 {
		return fmt.Errorf("write render: nothing to render")
	}

	for i, phase := range r.Phases {
		dir := filepath.Join(root, fmt.Sprintf("%02d-%s", i+1, phase.Name))

		for _, file := range phase.Files {
			path := filepath.Join(dir, file.Path)

			if dryRun {
				fmt.Println(path)
				continue
			}

			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return fmt.Errorf("create directory for %s: %w", path, err)
			}

			if file.YAML {
				if err := structured.WriteFileAndFormat(path, file.Content, 0644); err != nil {
					return fmt.Errorf("write YAML %s: %w", path, err)
				}
			} else {
				if err := os.WriteFile(path, file.Content, 0644); err != nil {
					return fmt.Errorf("write %s: %w", path, err)
				}
			}
		}
	}

	return nil
}
