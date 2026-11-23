package cli

import (
	"fmt"

	"github.com/intentd/jig"
	"github.com/intentd/tools/term"
	"github.com/spf13/cobra"
)

func planCmd() *cobra.Command {
	var file string
	var format string
	var output string

	cmd := &cobra.Command{
		Use:   "plan [file]",
		Short: "Show what a jig would do without running it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := target(args, file)

			d, _, err := load(path)
			if err != nil {
				return err
			}

			f := jig.PlanFormat(format)
			if !f.IsValid() {
				return fmt.Errorf("unsupported plan format %q", format)
			}

			result, err := jig.Plan(d)
			if err != nil {
				return err
			}

			if output != "" {
				return result.WriteToFile(output, f.String())
			}

			out := cmd.OutOrStdout()

			if f == jig.PlanFormatText {
				fmt.Fprintf(out, "\n%s\n\n", term.Bold("Planning..."))
			}

			return result.Write(out, f.String())
		},
	}

	fileFlag(cmd, &file)
	cmd.Flags().StringVar(&format, "format", "text", "plan format: text or json")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write plan to a file")

	return cmd
}
