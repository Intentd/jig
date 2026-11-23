package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/intentd/jig"
	"github.com/intentd/tools/fileio"
	"github.com/intentd/tools/term"
)

func validateCmd() *cobra.Command {
	var file string
	var disc discoveryFlags
	cmd := &cobra.Command{
		Use:   "validate [file]",
		Short: "Check that a jig is valid",
		Long: `Read the test, expand its includes, and validate it.
Every problem is reported, and nothing touches a cluster.

./... (or dir/...) checks every jig.yaml below, one line each.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			path := target(args, file)

			if root, ok := fileio.IsDiscoveryArg(path); ok {
				paths, err := discover(out, root, disc.skip)
				if err != nil || len(paths) == 0 {
					return err
				}
				return validateDiscovered(out, root, paths)
			}

			fmt.Fprintf(out, "\n%s\n\n", term.Bold("Validating..."))

			decl, _, err := load(path)
			if err != nil {
				return err
			}

			// Build the plan to ensure all plan-time resolution checks pass.
			if _, err := jig.Plan(decl); err != nil {
				return err
			}

			fmt.Fprintf(out, "\n%s %s\n",
				term.SuccessMark(),
				term.Green("Jig is valid: "+path),
			)
			return nil
		},
	}

	fileFlag(cmd, &file)
	disc.add(cmd, false)
	return cmd
}
