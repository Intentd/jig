package render

import (
	"fmt"
	"path/filepath"

	"github.com/intentd/jig/pkg/spec"
)

// renderPhase renders a phase for this declaration and recursively includes
// the same phase from imported declarations.
func renderPhase(
	d *spec.Declaration,
	render func(*spec.Declaration) (*spec.RenderPhase, error),
) (*spec.RenderPhase, error) {
	phase, err := render(d)
	if err != nil {
		return nil, err
	}

	if d == nil || !d.IsAggregator() {
		return phase, nil
	}

	for i, imp := range d.Imports {
		decl, err := imp.Read(d.Dir)
		if err != nil {
			return nil, fmt.Errorf("read import %d (%s): %w", i, imp.Path, err)
		}

		importPhase, err := render(decl)
		if err != nil {
			return nil, fmt.Errorf("render import %q: %w", imp.Path, err)
		}

		for j := range importPhase.Files {
			importPhase.Files[j].Path = filepath.Join(
				fmt.Sprintf("%02d-%s", i, decl.Name),
				importPhase.Files[j].Path,
			)
		}

		phase.Files = append(phase.Files, importPhase.Files...)
	}

	return phase, nil
}
