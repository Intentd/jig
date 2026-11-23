package cli

import (
	"fmt"

	"github.com/intentd/jig/cmd/cli/scaffold"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [project-name]",
		Short: "Initialize a new Jig project",
		Args: func(cmd *cobra.Command, args []string) error {
			list, _ := cmd.Flags().GetBool("list")
			clear, _ := cmd.Flags().GetBool("clear-cache")

			if list || clear {
				return nil
			}

			if len(args) > 1 {
				return fmt.Errorf("too many arguments — usage: jig init [project-name]")
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _ := cmd.Flags().GetBool("list")
			if list {
				return scaffold.ListPacks()
			}

			clear, _ := cmd.Flags().GetBool("clear-cache")
			if clear {
				return scaffold.ClearCache()
			}

			name := "."
			if len(args) == 1 {
				name = args[0]
			}

			pack, _ := cmd.Flags().GetString("pack")
			refresh, _ := cmd.Flags().GetBool("refresh-cache")

			if pack == "" {
				return scaffold.Canonical(name)
			}

			return scaffold.Project(name, pack, refresh)
		},
	}

	cmd.Flags().StringP("pack", "p", "", "Example pack to initialize from")
	cmd.Flags().BoolP("list", "l", false, "List available example packs")
	cmd.Flags().Bool("clear-cache", false, "Clear cached example packs")
	cmd.Flags().Bool("refresh-cache", false, "Download the example pack from GitHub Releases instead of using the embedded copy")

	return cmd
}
