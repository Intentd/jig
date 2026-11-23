package cli

import (
	"fmt"

	"github.com/intentd/jig"
	"github.com/intentd/tools/term"
	"github.com/spf13/cobra"
)

func renderCmd() *cobra.Command {
	var file string
	var output string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "render [file]",
		Short: "Render the files needed to reproduce a jig run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := target(args, file)

			d, _, err := load(path)
			if err != nil {
				return err
			}

			result, err := jig.Render(d)
			if err != nil {
				return err
			}

			if output == "" {
				output = ".jig"
			}

			if err := result.Write(output, dryRun); err != nil {
				return err
			}

			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry run: no files written\n")
				return nil
			}

			fmt.Fprintf(
				cmd.OutOrStdout(),
				term.Green("Rendered reproduction files to → %s/\n"),
				term.Bold(output),
			)

			return nil
		},
	}

	fileFlag(cmd, &file)
	cmd.Flags().StringVarP(&output, "output", "o", ".jig", "directory to write rendered files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show files that would be rendered without writing them")

	return cmd
}
