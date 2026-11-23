package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/intentd/jig"
)

// defaultFile is the test a command reads when none is given.
const defaultFile = "jig.yaml"

// fileFlag adds -f/--file to cmd.
func fileFlag(cmd *cobra.Command, file *string) {
	cmd.Flags().StringVarP(file, "file", "f", "", "the test to read, a directory with a "+defaultFile+", or ./... to find them all (default "+defaultFile+"; a positional argument works too)")
}

// target is what a command reads: the positional argument, else -f, else
// jig.yaml.
func target(args []string, file string) string {
	switch {
	case len(args) > 0:
		return args[0]
	case file != "":
		return file
	}
	return defaultFile
}

// load reads the test at path. A directory means its jig.yaml.
func load(path string) (*jig.Declaration, string, error) {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		path = filepath.Join(path, defaultFile)
	}
	d, err := jig.FromFile(path)
	return d, path, err
}
