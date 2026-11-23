package scaffold

import (
	"path/filepath"
	"sort"

	"github.com/intentd/jig/examples"
)

// Pack is an example directory that `jig init` can extract.
type Pack struct {
	Name        string
	Description string
	Path        string // directory inside the embedded FS
	First       string // example to start with, relative to Path
	Order       int    // position in --list
}

var Packs = map[string]Pack{
	"commands": {
		Name:        "commands",
		Description: "Run commands and verify their results, without a cluster.",
		Path:        "commands",
		Order:       1,
	},
	"app-image": {
		Name:        "app-image",
		Description: "Build an application image and prove it serves before pushing.",
		Path:        "app-image",
		Order:       2,
	},
	"helm-chart": {
		Name:        "helm-chart",
		Description: "Install a Helm chart, upgrade it, and verify the result.",
		Path:        "helm-chart",
		Order:       3,
	},
	"manifests": {
		Name:        "manifests",
		Description: "Run a migration Job, verify it, and clean it up.",
		Path:        "manifests",
		Order:       4,
	},
	"stack": {
		Name:        "stack",
		Description: "Run multiple dependent services together on Kubernetes.",
		Path:        "stack",
		Order:       5,
	},
	"ecosystem": {
		Name:        "ecosystem",
		Description: "Run an application with operator-managed data services.",
		Path:        "ecosystem",
		Order:       6,
	},
	"library": {
		Name:        "library",
		Description: "Use Jig from Go: load, validate, plan, run, and report.",
		Path:        "library",
		First:       "",
		Order:       7,
	},
	"suite": {
		Name:        "suite",
		Description: "Run the examples together as one suite.",
		Path:        ".",
		First:       "jig.yaml",
		Order:       8,
	},
}

// getPack returns a named pack, or a valid directory inside the embedded
// examples filesystem.
func getPack(name string) (Pack, bool) {
	if p, ok := Packs[name]; ok {
		return p, true
	}

	// Sub-path fallback: any valid directory in the embedded FS works as a pack.
	// For example, `jig init my-project --pack stack` extracts into my-project.
	f, err := examples.FS.Open(name)
	if err != nil {
		return Pack{}, false
	}
	defer f.Close()

	return Pack{
		Name: filepath.Base(name),
		Path: name,
	}, true
}

// listPacks returns the available packs in display order.
func listPacks() []Pack {
	out := make([]Pack, 0, len(Packs))
	for _, p := range Packs {
		out = append(out, p)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})

	return out
}

func (p Pack) String() string {
	if p.First == "" {
		return p.Name + " — " + p.Description + " (examples/" + p.Path + ")"
	}

	return p.Name + " — " + p.Description +
		" (start with: examples/" + filepath.Join(p.Path, p.First) + ")"
}
