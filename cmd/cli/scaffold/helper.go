package scaffold

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/intentd/jig/examples"
	toolsscaffold "github.com/intentd/tools/scaffold"
	"github.com/intentd/tools/term"
	"github.com/intentd/tools/version"
)

// extractEmbeddedPack extracts a pack bundled with Jig into the project.
func extractEmbeddedPack(root, pack string) error {
	p, ok := getPack(pack)
	if !ok {
		return fmt.Errorf("unknown pack %q", pack)
	}

	return toolsscaffold.ExtractPack(
		root,
		examples.FS,
		toolsscaffold.Pack{
			Name:  p.Name,
			Path:  p.Path,
			First: p.First,
		},
		toolsscaffold.Options{
			GoModReplacements: map[string]string{
				"{{ .JigVersion }}": version.Short(),
			},
		},
	)
}

// extractCanonical copies the canonical app-image example into the project root.
func extractCanonical(root string) error {
	return toolsscaffold.ExtractCanonical(
		root,
		examples.FS,
		"app-image",
		[]toolsscaffold.CanonicalFile{
			{Source: "jig.yaml", Destination: "jig.yaml"},
			{Source: "Dockerfile", Destination: "Dockerfile"},
			{Source: "index.html", Destination: "index.html"},
		},
	)
}

// downloadExamplePack downloads a release archive or returns its cached path.
func downloadExamplePack(pack, version string, refresh bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return toolsscaffold.DownloadPack(pack, toolsscaffold.DownloadOptions{
		Repository: "intentd/jig",
		Version:    version,
		CacheDir:   filepath.Join(home, ".jig", "packs"),
		Refresh:    refresh,
	})
}

// extractExamplePack extracts a downloaded pack archive into the project.
func extractExamplePack(root, pack, archivePath string) error {
	return toolsscaffold.ExtractPackArchive(root, pack, archivePath)
}

// ClearCache removes all cached Jig pack archives.
func ClearCache() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	return toolsscaffold.ClearCache(filepath.Join(home, ".jig", "packs"))
}

type initStep struct {
	name string
	fn   func() error
}

// runSteps executes initialization steps and prints their results.
func runSteps(steps []initStep) error {
	for _, step := range steps {
		fmt.Printf("  %-50s", step.name+"...")
		if err := step.fn(); err != nil {
			fmt.Printf("%s\n", term.FailureMark())
			return fmt.Errorf("%s: %w", step.name, err)
		}
		fmt.Printf("%s\n", term.SuccessMark())
	}

	return nil
}

// printProjectReady prints the project initialization completion message.
func printProjectReady(name, label string) {
	projectPrint := "✓ Project ready: " + label
	if isCurrentDirectory(name) {
		projectPrint = "✓ Project ready"
	}

	fmt.Printf("\n%s\n\n", term.Green(projectPrint))
}

// nameLabel returns the display name for a project path.
func nameLabel(name string) string {
	if !isCurrentDirectory(name) {
		return name
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}

	return filepath.Base(cwd)
}

// isCurrentDirectory reports whether the project name refers to the current
// working directory.
func isCurrentDirectory(name string) bool {
	return name == "."
}
