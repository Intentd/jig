package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/intentd/jig"
	"github.com/intentd/tools/cluster"
)

func cleanCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "clean [file]",
		Short: "Delete the clusters a run kept",
		Long: `Delete the kind clusters a run left behind on purpose (--keep or
--keep=cluster): the jig's cluster, its <name>-imports cluster, and those of its
imports. A cluster that doesn't exist is skipped. Nothing else is touched.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, path, err := load(target(args, file))
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			deleted := 0
			for _, name := range clusters(d, filepath.Dir(path), map[string]bool{}) {
				if !cluster.KindExists(ctx, name) {
					continue
				}
				fmt.Fprintf(out, "→ Deleting cluster '%s'...\n", name)
				if err := cluster.DeleteKind(ctx, name, cluster.KindOptions{}); err != nil {
					return fmt.Errorf("deleting cluster %s: %w", name, err)
				}
				deleted++
			}
			if deleted == 0 {
				fmt.Fprintln(out, "Nothing to clean")
			}
			return nil
		},
	}
	fileFlag(cmd, &file)
	return cmd
}

// clusters lists the kind clusters runs of t can keep: its own, its
// -imports cluster, and its imports', each once.
func clusters(d *jig.Declaration, dir string, seen map[string]bool) []string {
	var out []string
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if d.Environment.Provider() != jig.ProviderNone {
		name := d.Environment.ClusterName("jig")
		add(name)
		add(name + "-imports")
	}
	for _, imp := range d.Imports {
		path := imp.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		sub, err := jig.FromFile(path)
		if err != nil {
			continue // run reports it; clean deletes what it can name
		}
		out = append(out, clusters(sub, filepath.Dir(path), seen)...)
	}
	return out
}
