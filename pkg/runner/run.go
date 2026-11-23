// Package runner runs a jig: it provides the cluster, loads the images,
// provisions the environment, runs the prepare steps in order with the
// expectations that follow each, runs the imports, tears down what it did,
// and writes the report.
package runner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/intentd/jig/pkg/check"
	"github.com/intentd/jig/pkg/ecosystem"
	"github.com/intentd/jig/pkg/expect"
	"github.com/intentd/jig/pkg/expect/report"
	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/jig/pkg/post"
	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/term"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	startSpinner = term.StartSpinner
	successMark  = term.SuccessMark
	failureMark  = term.FailureMark
)

// Options configure a run.
type Options struct {
	// Context runs against this kubectl context instead of creating a cluster.
	Context string
	// UseCurrent runs against the current context instead of creating a cluster.
	UseCurrent bool
	// KeepCluster (--keep=cluster) keeps a cluster the run creates; what the
	// run did is still torn down.
	KeepCluster bool
	// Keep (--keep) leaves everything as the run left it: no teardown, the
	// cluster kept.
	Keep bool

	// Workers and KindVersion shape a cluster the run creates; KindBinDir is
	// where the kind binary is kept ("" is kind's default).
	Workers     int
	KindVersion string
	KindBinDir  string
	// DefaultClusterName names the cluster when the test names none; "" is
	// "jig".
	DefaultClusterName string

	// Images narrows environment.images; with none declared, it is the list.
	Images []string
	// Parallel is how many images build at once; 0 is the default
	// (min(4, CPUs/2)) and 1 builds them one after another.
	Parallel int
	// EcosystemChart is the data-services chart to install the ecosystem
	// with: an oci:// reference or a local directory, for chart work.
	// Empty is the chart jig pins.
	EcosystemChart string
	// Functions are the template functions expectations start from; nil is
	// KEL's. The jig's functions are added to them.
	Functions template.FuncMap
	// Env is a parent's env, inherited as defaults by an import.
	Env         map[string]string
	MaxFailures int
	// DryRun sends every write to the cluster as a server dry run.
	DryRun bool

	// ReportFile writes the report there too: JSON for .json, else Markdown.
	ReportFile string
	// Caller names who ran the test in the report ("jig" by default).
	Caller string
	// Command and SpecPath are shown in the report.
	Command, SpecPath string
	// FieldManager owns what the run applies server-side; "" is "jig".
	FieldManager string

	// Load reads an import; nil is check.FromFile. A caller whose imports are
	// its own format maps them here.
	Load func(path string) (*spec.Declaration, error)

	// shared marks an import on its parent's cluster: what its steps with
	// keep did is added to parentUndos, for the parent to undo after its
	// last import.
	// suppressReport leaves the report to the parent.
	shared         bool
	parentUndos    *[]post.Undo
	suppressReport bool
	// suiteEcosystem is the ecosystem the imports on a shared cluster
	// share: one plan, installed once per cluster.
	suiteEcosystem *suiteEcosystem
}

// runner is one run's state.
type runner struct {
	spec  *spec.Declaration
	o     Options
	undos []post.Undo

	// The ecosystem's plan, and what the resolvers read from it.
	ecosystem    *ecosystem.Plan
	contexts     spec.Contexts
	templateData map[string]any

	needsCluster bool
	cs           kubernetes.Interface
	cfg          *rest.Config
	dyn          dynamic.Interface
	mapper       apimeta.RESTMapper
}

// Run runs s and returns its result. The error is set when the run could
// not complete or an expectation failed.
func Run(ctx context.Context, s *spec.Declaration, o Options) (*expect.Result, error) {
	if o.Load == nil {
		o.Load = check.FromFile
	}
	if o.DefaultClusterName == "" {
		o.DefaultClusterName = "jig"
	}
	if o.FieldManager == "" {
		o.FieldManager = "jig"
	}
	if o.Keep {
		o.KeepCluster = true
	}
	if len(o.Env) > 0 {
		c := *s
		c.Env = s.Env.Inherit(o.Env)
		s = &c
	}
	r := &runner{spec: s, o: o}
	return r.run(ctx)
}

func (r *runner) run(ctx context.Context) (*expect.Result, error) {
	s, o := r.spec, r.o
	start := time.Now()
	if s.Description != "" {
		fmt.Printf("\nRunning %s — %s\n\n", s.Name, s.Description)
	} else {
		fmt.Printf("\nRunning %s\n\n", s.Name)
	}

	if err := r.planEcosystem(); err != nil {
		return nil, fmt.Errorf("ecosystem: %w", err)
	}

	r.needsCluster = s.Environment.Provider() != spec.ProviderNone
	ownsCluster := o.Context == "" && !o.UseCurrent
	deleteCluster := ownsCluster && !o.KeepCluster && r.needsCluster

	if o.DryRun {
		switch {
		case !r.needsCluster:
			return nil, fmt.Errorf("--dry-run=server sends writes to a cluster, but this test runs without one (cluster.provider: none)")
		case ownsCluster:
			return nil, fmt.Errorf("--dry-run=server needs an existing cluster: use --use-current or --context")
		}
		kubectl.SetDryRun(true)
		defer kubectl.SetDryRun(false)
		fmt.Println("→ Dry run (server): writes are checked by the API server, not applied; commands and assertions are skipped")
	}

	clusterDeleted := false
	if r.needsCluster {
		if restore := preserveKubectlContext(ctx); restore != nil {
			// A kept cluster is there to be looked at: the context stays on
			// it. An import always gives its parent's context back.
			defer func() {
				if o.KeepCluster && !o.suppressReport {
					fmt.Printf("\n→ kubectl context left on %q, the kept cluster\n", r.effectiveContext(context.Background()))
					return
				}
				restore()
			}()
		}
		// What the run did is undone unless the cluster goes with it, or
		// everything is kept.
		if !o.Keep && !deleteCluster {
			defer func() { post.Teardown(context.Background(), r.undos) }()
		}
		if deleteCluster {
			defer func() {
				if !clusterDeleted {
					fmt.Printf("\n→ Deleting cluster '%s' (run stopped early)...\n", r.clusterName())
					if err := r.deleteCluster(ctx); err != nil {
						fmt.Printf("  ! Could not delete cluster: %v\n", err)
					}
				}
			}()
		}
		if err := r.ensureCluster(ctx); err != nil {
			return nil, fmt.Errorf("cluster: %w", err)
		}
		if err := r.buildClient(); err != nil {
			return nil, fmt.Errorf("kubernetes client: %w", err)
		}
		// The tools the run needs (git for an image from a repository)
		// before anything uses them.
		if err := ensureTools(s); err != nil {
			return nil, err
		}
		// Images are built and loaded first (slow, and independent of the
		// rest); deployed only after the Secrets they read.
		if err := r.loadImages(ctx); err != nil {
			return nil, err
		}
		if err := r.installEcosystem(ctx); err != nil {
			return nil, err
		}
		if err := r.deployImages(ctx); err != nil {
			return nil, err
		}
	}

	finallyRan := false
	runFinally := func() []string {
		if finallyRan || s.Finally == nil || kubectl.DryRun() {
			return nil
		}
		finallyRan = true
		return expect.SuitePost(ctx, "finally", &s.Finally.SideEffects, r.postOptions())
	}
	defer runFinally()

	aggregator := len(s.Prepare) == 0 && len(s.Expect) == 0 && len(s.Imports) > 0
	exp := expect.NewRunner(expect.Options{
		WorkDir: s.Dir, MaxFailures: o.MaxFailures,
		CS: r.cs, Cfg: r.cfg, Dynamic: r.dyn, Mapper: r.mapper,
		Env: s.Env, Functions: o.Functions, SpecFunctions: s.Functions.Functions(),
		OnFailure: s.OnFailure, Data: s.Data,
		Contexts: r.contexts, TemplateData: r.templateData,
	})

	newResult := func() *expect.Result {
		return &expect.Result{
			Caller:    o.Caller,
			Name:      s.Name,
			Cases:     exp.Cases(),
			StartedAt: start,
			Elapsed:   time.Since(start),
			Command:   o.Command,
			Planned:   len(s.Expect),
			SpecPath:  o.SpecPath,
			Context:   o.Context,
			Cluster:   r.clusterName(),
		}
	}

	// The environment or a step failed: the report still says where and
	// why, with the expectations that ran before it.
	at, err := r.environmentAndSteps(ctx, exp)
	// Guards look at the whole run, after the steps, failed or not: what
	// went wrong that no expectation asked about.
	r.runGuards(ctx, exp, start)
	if err != nil {
		result := newResult()
		result.SetupFailure = &expect.SetupFailure{At: at, Err: err.Error()}
		result.Warnings = r.suiteOnFailure(ctx, aggregator)
		result.Warnings = append(result.Warnings, runFinally()...)
		if !o.suppressReport {
			if werr := report.Write(ctx, o.ReportFile, result); werr != nil {
				err = errors.Join(err, fmt.Errorf("writing report: %w", werr))
			}
		}
		return result, err
	}

	result := newResult()
	failures := exp.Failures()
	stopped, stopReason := exp.Stopped()
	if stopped != nil {
		result.StoppedAfter, result.StopReason = failures, stopReason
		fmt.Printf("\n  ! stopped after %d failure(s) (%s): %d of %d expectations run\n",
			failures, stopReason, result.Total(), result.Planned)
	}
	if !aggregator {
		r.printReport(result)
	}
	if !result.AllPassed() {
		result.Warnings = r.suiteOnFailure(ctx, aggregator)
	}

	var runErr error
	switch {
	case stopped != nil:
		runErr = fmt.Errorf("%d of %d expectations failed (%d of %d run): %w",
			result.Failed(), result.Total(), result.Total(), result.Planned, stopped)
	case !result.AllPassed():
		runErr = fmt.Errorf("%d of %d expectations failed", result.Failed(), result.Total())
	}

	if n := len(s.Imports); n > 0 {
		fmt.Printf("\n─── Running %d import(s) ───\n", n)
		result.Imports = r.runImports(ctx, aggregator)
		if err := result.Imports.PrintImportSummary(s.Name); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}

	result.Warnings = append(result.Warnings, runFinally()...)

	if deleteCluster {
		clusterDeleted = true
		fmt.Printf("\n→ Deleting cluster '%s'...\n", r.clusterName())
		if err := r.deleteCluster(ctx); err != nil {
			fmt.Printf("  ! Could not delete cluster: %v\n", err)
			runErr = errors.Join(runErr, fmt.Errorf("deleting cluster: %w", err))
		} else {
			fmt.Printf("  %s Cluster deleted\n", successMark())
		}
	}
	switch {
	case o.Keep:
		fmt.Println("\n→ Keeping everything (--keep)")
	case o.KeepCluster && ownsCluster && r.needsCluster:
		fmt.Println("\n→ Keeping cluster (--keep=cluster)")
	}

	if !o.suppressReport {
		if err := report.Write(ctx, o.ReportFile, result); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("writing report: %w", err))
		}
	}
	return result, runErr
}

// environmentAndSteps provisions the environment, runs the expectations
// after it, then each step and the expectations after that step, then those
// after prepare. It stops early when an expectation stops the run.
func (r *runner) environmentAndSteps(ctx context.Context, exp *expect.Runner) (at string, err error) {
	s := r.spec
	groups := map[spec.After]spec.Expectations{}
	for _, e := range s.Expect {
		after := e.After
		if after == "" {
			after = spec.AfterPrepare
		}
		groups[after] = append(groups[after], e)
	}

	if err := ensureTools(s); err != nil {
		return setupEnvironment, err
	}
	if r.needsCluster && !r.o.DryRun {
		fmt.Println("→ Ensuring dependencies...")
		if err := cluster.EnsureDependencies(); err != nil {
			return setupEnvironment, fmt.Errorf("dependencies: %w", err)
		}
		if env := s.Environment; env != nil {
			if err := r.provision(ctx, "environment", env.Provision); err != nil {
				return setupEnvironment, err
			}
		}
	}
	if exp.Run(ctx, groups[spec.AfterEnvironment]) {
		return "", nil
	}
	for i, st := range s.Prepare {
		if r.needsCluster || len(st.Commands) > 0 {
			if err := r.step(ctx, i, st); err != nil {
				return stepLabel(i, st), err
			}
		}
		if st.Name != "" && exp.Run(ctx, groups[spec.After(st.Name)]) {
			return "", nil
		}
	}
	exp.Run(ctx, groups[spec.AfterPrepare])
	return "", nil
}

// postOptions are what the jig's onFailure and finally run with.
func (r *runner) postOptions() expect.SuitePostOptions {
	return expect.SuitePostOptions{
		Env: r.spec.Env, WorkDir: r.spec.Dir, CS: r.cs, Cfg: r.cfg,
		Functions: r.o.Functions, SpecFunctions: r.spec.Functions.Functions(),
		Contexts: r.contexts, TemplateData: r.templateData,
	}
}

// suiteOnFailure runs the jig's onFailure once: not for an aggregator,
// whose imports run their own, nor under a server dry run.
func (r *runner) suiteOnFailure(ctx context.Context, aggregator bool) []string {
	if aggregator || kubectl.DryRun() || r.spec.OnFailure == nil {
		return nil
	}
	return expect.SuitePost(ctx, "onFailure", &r.spec.OnFailure.SideEffects, r.postOptions())
}

// abs resolves path against the jig's directory; a URL stays as it is.
func (r *runner) abs(path string) string {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "://") {
		return path
	}
	// Absolute, so a local chart still reads as one (a relative test
	// directory would make ./chart into dir/chart, which Helm takes for a
	// repository chart).
	if abs, err := filepath.Abs(filepath.Join(r.spec.Dir, path)); err == nil {
		return abs
	}
	return filepath.Join(r.spec.Dir, path)
}
