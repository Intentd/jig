package render

import (
	"fmt"
	"path/filepath"

	"github.com/intentd/jig/pkg/spec"
)

// prepare renders the artifacts needed by prepare steps.
func prepare(d *spec.Declaration) (*spec.RenderPhase, error) {
	return renderPhase(d, func(d *spec.Declaration) (*spec.RenderPhase, error) {
		phase := &spec.RenderPhase{Name: "prepare"}
		if d == nil {
			return phase, nil
		}

		for i, step := range d.Prepare {
			stepDir := filepath.Join("prepare", fmt.Sprintf("%02d-%s", i, step.Name))

			applyFiles, err := renderApply(step.Apply, d.Dir)
			if err != nil {
				return nil, fmt.Errorf("prepare step %q: render apply: %w", step.Name, err)
			}

			for j := range applyFiles {
				applyFiles[j].Path = filepath.Join(stepDir, applyFiles[j].Path)
			}
			phase.Files = append(phase.Files, applyFiles...)

			if step.Helm != nil {
				helmFiles, err := renderHelmInstall(spec.HelmInstalls{*step.Helm}, d.Dir)
				if err != nil {
					return nil, fmt.Errorf("prepare step %q: render Helm install: %w", step.Name, err)
				}

				for j := range helmFiles {
					helmFiles[j].Path = filepath.Join(stepDir, helmFiles[j].Path)
				}
				phase.Files = append(phase.Files, helmFiles...)
			}
		}

		return phase, nil
	})
}
