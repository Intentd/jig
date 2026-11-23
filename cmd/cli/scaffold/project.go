package scaffold

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/intentd/jig/examples"
	"github.com/intentd/tools/term"
	"github.com/intentd/tools/version"
)

// ListPacks prints the available embedded example packs.
func ListPacks() error {
	fmt.Println("Available example packs:")
	for _, p := range listPacks() {
		fmt.Printf("  %-25s → %s\n", p.Name, p.Description)
	}

	fmt.Println("\nDefault project:")
	fmt.Println("  jig init              # initialize in the current directory")
	fmt.Println("  jig init my-project   # initialize in a new directory")

	fmt.Println("\nUsing an example pack:")
	fmt.Println("  jig init --pack <name>")
	fmt.Println("  jig init my-project --pack <name>")

	return nil
}

// Canonical creates a project from Jig's canonical app-image example.
func Canonical(name string) error {
	label := nameLabel(name)
	fmt.Printf("Initializing %s...\n\n", term.Bold(label))

	steps := []initStep{}

	if !isCurrentDirectory(name) {
		steps = append(steps, initStep{
			name: "Creating project folder",
			fn: func() error {
				return os.MkdirAll(name, 0755)
			},
		})
	}

	steps = append(steps, initStep{
		name: "Creating app-image project",
		fn: func() error {
			return extractCanonical(name)
		},
	})

	if err := runSteps(steps); err != nil {
		return err
	}

	printProjectReady(name, label)

	fmt.Println("To run your project:")
	if !isCurrentDirectory(name) {
		fmt.Printf("  cd %s\n", name)
	}
	fmt.Printf("  jig run\n\n")

	fmt.Println("To explore example packs:")
	fmt.Println("  jig init --list")
	fmt.Println("  jig init --pack <name>")

	return nil
}

// Project creates a project from an embedded or downloaded example pack.
func Project(name, pack string, refresh bool) error {
	p, ok := getPack(pack)
	if !ok {
		return fmt.Errorf("unknown pack: %s", pack)
	}

	label := nameLabel(name)

	fmt.Printf("Initializing %s using '%s' example pack...\n\n",
		term.Bold(label), pack)

	if err := createProjectFolder(name); err != nil {
		return err
	}

	if err := extractProjectPack(name, pack, refresh); err != nil {
		return err
	}

	printProjectReady(name, label)
	printExampleInstructions(name, p)

	return nil
}

// createProjectFolder creates the target directory when needed.
func createProjectFolder(name string) error {
	if isCurrentDirectory(name) {
		return nil
	}

	return runSteps([]initStep{
		{
			name: "Creating project folder",
			fn: func() error {
				return os.MkdirAll(name, 0755)
			},
		},
	})
}

// extractProjectPack downloads and extracts a pack, or extracts its embedded version.
func extractProjectPack(name, pack string, refresh bool) error {
	if refresh {
		var archivePath string

		return runSteps([]initStep{
			{
				name: "Downloading example pack",
				fn: func() error {
					var err error
					archivePath, err = downloadExamplePack(
						pack,
						version.Version,
						refresh,
					)
					return err
				},
			},
			{
				name: "Extracting example pack",
				fn: func() error {
					return extractExamplePack(name, pack, archivePath)
				},
			},
		})
	}

	return runSteps([]initStep{
		{
			name: "Extracting example pack",
			fn: func() error {
				return extractEmbeddedPack(name, pack)
			},
		},
	})
}

// resolveExampleDir determines the directory containing the example entry point.
func resolveExampleDir(p Pack) (string, error) {
	dir := p.Path
	if dir == "." {
		dir = ""
	}

	if p.First == "" {
		return dir, nil
	}

	first := filepath.Join(dir, p.First)

	sourcePath := filepath.ToSlash(filepath.Join(p.Path, p.First))
	info, err := fs.Stat(examples.FS, sourcePath)
	if err != nil {
		return "", fmt.Errorf("resolve example entry point: %w", err)
	}

	if !info.IsDir() {
		first = filepath.Dir(first)
	}

	dir = first
	if dir == "." {
		dir = ""
	}

	return dir, nil
}

// printExampleInstructions prints the commands needed to explore and run a pack.
func printExampleInstructions(name string, p Pack) {
	dir, err := resolveExampleDir(p)
	if err != nil {
		fmt.Printf("Could not resolve example directory: %v\n", err)
		return
	}

	fmt.Println("To explore and run the example:")

	if !isCurrentDirectory(name) {
		target := filepath.Join(name, dir)
		if target == "." {
			target = name
		}
		fmt.Printf("  cd %s\n", target)
	}

	if p.Name == "library" {
		fmt.Println("  make run")
	} else {
		fmt.Println("  jig run")
	}
}
