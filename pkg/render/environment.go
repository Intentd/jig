package render

import (
	"fmt"
	"github.com/intentd/jig/pkg/spec"
)

// environment builds the Kubernetes manifests produced by the
// environment's declared images and, for aggregators, their imports.
//
// Images retain their declaration order. Their original indexes are used
// in filenames so that images without deployment configuration do not
// change the numbering of subsequent images.
//
// This method only builds artifacts. It does not write files or interact
// with a Kubernetes cluster.
func environment(d *spec.Declaration) (*spec.RenderPhase, error) {
	return renderPhase(d, func(d *spec.Declaration) (*spec.RenderPhase, error) {
		phase := &spec.RenderPhase{Name: "environment"}
		if d == nil || d.Environment == nil {
			return phase, nil
		}

		e := d.Environment

		applyFiles, err := renderApply(e.Apply, d.Dir)
		if err != nil {
			return nil, fmt.Errorf("render apply: %w", err)
		}
		phase.Files = append(phase.Files, applyFiles...)

		helmFiles, err := renderHelmInstall(e.Helm, d.Dir)
		if err != nil {
			return nil, fmt.Errorf("render Helm installs: %w", err)
		}
		phase.Files = append(phase.Files, helmFiles...)

		images, err := renderImages(e.Images, d.Name)
		if err != nil {
			return nil, fmt.Errorf("render images: %w", err)
		}
		phase.Files = append(phase.Files, images.Files...)

		return phase, nil
	})
}
