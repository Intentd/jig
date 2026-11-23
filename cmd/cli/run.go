package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/intentd/jig"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/fileio"
)

func runCmd() *cobra.Command {
	var file, context, kindVersion, dryRun, report, keep, ecosystemChart string
	var useCurrent bool
	var workers, maxFailures, parallel int
	var images []string
	var disc discoveryFlags
	cmd := &cobra.Command{
		Use:     "run [file]",
		Aliases: []string{"it"},
		Short:   "Run a jig and its imports, and write the report",
		Long: `Provide the cluster, build and load the images, provision the environment,
run the prepare steps in order with the expectations after each, run the
imports, tear down what the run did, and print the report.

By default a kind cluster is created and deleted afterwards. --use-current
runs in the current kubeconfig context and --context in the one named;
neither creates or deletes a cluster.

./... (or dir/...) finds every jig.yaml below and runs them as one suite on
one cluster, leaving out the ones that only import.`,
		Example: `  jig run
  jig run ./e2e/jig.yaml --context kind-dev
  jig run --images shop-api:dev --report      # also writes report.md
  jig run --ecosystem-chart ../charts/data-services   # a local data-services chart
  jig run --keep            # leave everything as the run left it
  jig run --keep=cluster    # keep the cluster, tear down what the run did
  jig run ./... --skip vendor --wait 2s
  jig run ./... --list`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			var d *jig.Declaration
			path := target(args, file)
			root, discovery := fileio.IsDiscoveryArg(path)
			if disc.list && !discovery {
				return fmt.Errorf("--list applies to ./... discovery, not a single test: jig run ./... --list")
			}
			if discovery {
				paths, err := discover(out, root, disc.skip)
				if err != nil || len(paths) == 0 {
					return err
				}
				if disc.list {
					listDiscovered(out, root, paths, disc.limit)
					return nil
				}
				fmt.Fprintf(out, "→ Discovered %d test(s) under %s\n", len(paths), root)
				for _, p := range paths {
					fmt.Fprintf(out, "    %s\n", rel(root, p))
				}
				d = jig.Suite("discovered-suite", paths, disc.wait)
			} else {
				var err error
				if d, path, err = load(path); err != nil {
					return err
				}
			}
			o := jig.Options{
				Context:        context,
				UseCurrent:     useCurrent,
				Workers:        workers,
				KindVersion:    kindVersion,
				Images:         images,
				MaxFailures:    maxFailures,
				EcosystemChart: ecosystemChart,
				Parallel:       parallel,
				ReportFile:     report,
				Command:        "jig " + strings.Join(os.Args[1:], " "),
				SpecPath:       path,
			}
			switch keep {
			case "":
			case "all":
				o.Keep = true
			case "cluster":
				o.KeepCluster = true
			default:
				return fmt.Errorf("--keep must be all or cluster, got %q", keep)
			}
			switch dryRun {
			case "":
			case "client":
				return jig.PlanAndReport(cmd.OutOrStdout(), d, jig.PlanFormatText.String())
			case "server":
				if !useCurrent && context == "" {
					if !cluster.ClusterReachable() {
						return fmt.Errorf("--dry-run=server sends writes to an existing cluster: none is reachable; add --use-current or --context <context>")
					}
					o.UseCurrent = true
				}
				o.DryRun = true
			default:
				return fmt.Errorf("--dry-run must be client or server, got %q", dryRun)
			}
			if o.UseCurrent && !cluster.ClusterReachable() {
				return fmt.Errorf("cannot reach a Kubernetes cluster: check your kubeconfig, or run without --use-current")
			}
			if context != "" && !cluster.CheckContext(context) {
				return fmt.Errorf("Kubernetes context %q does not exist in the current kubeconfig", context)
			}
			_, err := jig.Run(cmd.Context(), d, o)
			return err
		},
	}
	fileFlag(cmd, &file)
	disc.add(cmd, true)
	f := cmd.Flags()
	f.BoolVar(&useCurrent, "use-current", false, "run in the current kubeconfig context; no cluster is created or deleted")
	f.StringVarP(&context, "context", "c", "", "run in this kubeconfig context; no cluster is created or deleted")
	f.StringVar(&keep, "keep", "", "all (the default): leave everything as the run left it, no teardown, the cluster kept. cluster: keep only the cluster the run creates; what the run did is still torn down")
	f.Lookup("keep").NoOptDefVal = "all"
	f.IntVar(&workers, "workers", 0, "kind worker nodes for a cluster the run creates (0: control plane only)")
	f.StringVar(&kindVersion, "kind-version", "", "kind version to use (default: kind's own)")
	f.StringSliceVar(&images, "images", nil, "images to load: narrows environment.images, never adds to it; with none declared, the list (repeatable or comma-separated)")
	f.StringVar(&ecosystemChart, "ecosystem-chart", "", "the data-services chart for environment.ecosystem: an oci:// reference or a local chart directory (default: the chart jig pins)")
	f.IntVar(&parallel, "parallel", 0, "how many images build at once (default: min(4, CPUs/2)); 1 builds them one after another")
	f.IntVar(&maxFailures, "max-failures", 0, "stop after N failed expectations (0: run all)")
	f.StringVar(&dryRun, "dry-run", "", "client: print what would run. server: send the writes to an existing cluster as server dry runs, skipping commands and assertions")
	f.Lookup("dry-run").NoOptDefVal = "client"
	f.StringVarP(&report, "report", "r", "", "also write the report to this file: JSON for .json, else Markdown (default report.md)")
	f.Lookup("report").NoOptDefVal = "report.md"
	return cmd
}
