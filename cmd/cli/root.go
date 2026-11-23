package cli

import "github.com/spf13/cobra"

// Command returns the jig command: validate, run, clean and version.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "jig",
		Short:         "Run declarative Kubernetes tests that own their environment",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(
		initCmd(), validateCmd(),
		planCmd(), renderCmd(),
		runCmd(), cleanCmd(),
		versionCmd(),
	)
	return cmd
}
