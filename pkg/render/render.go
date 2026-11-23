package render

import (
	"fmt"

	"github.com/intentd/jig/pkg/spec"
)

// Build constructs the artifacts needed to reproduce a Jig run.
// Phases are rendered in execution order. It does not write files
// or interact with a Kubernetes cluster.
// Render builds the artifacts needed to reproduce a Jig run.
func Build(d *spec.Declaration) (*spec.Render, error) {
	if d == nil {
		return nil, fmt.Errorf("render declaration: declaration is nil")
	}

	ren := &spec.Render{}

	environment, err := environment(d)
	if err != nil {
		return nil, fmt.Errorf("render environment: %w", err)
	}

	if environment != nil {
		ren.Phases = append(ren.Phases, *environment)
	}

	prepare, err := prepare(d)
	if err != nil {
		return nil, fmt.Errorf("render prepare: %w", err)
	}

	if prepare != nil {
		ren.Phases = append(ren.Phases, *prepare)
	}

	return ren, nil
}
