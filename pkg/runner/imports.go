package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/intentd/jig/pkg/expect"
	"github.com/intentd/jig/pkg/post"
	"github.com/intentd/kel/core"
	"github.com/intentd/tools/cluster"
)

// runImports runs each import after the test.
//
// An import with freshCluster gets its own cluster. The others share one:
//   - an aggregator (a jig that only imports) lends its cluster;
//   - a jig run on --context or --use-current lends that one;
//   - a jig that created its own cluster and ran gives its imports a
//     separate <name>-imports cluster, so they don't run beside it.
//
// A shared import undoes what it did, except its steps with keep: those the
// parent undoes after its last import, so the imports after it still have
// them (a release later imports upgrade is uninstalled once).
func (r *runner) runImports(ctx context.Context, aggregator bool) expect.ImportResults {
	s, o := r.spec, r.o
	sharedCtx := o.Context
	useCurrent := o.UseCurrent || (o.Context == "" && aggregator)
	var parentUndos []post.Undo

	if r.needsCluster && o.Context == "" && !o.UseCurrent && !aggregator && r.hasSharedImports() {
		name := r.clusterName() + "-imports"
		fmt.Printf("→ Creating imports cluster '%s'...\n", name)
		if err := cluster.EnsureKind(ctx, name, r.kindOptions()); err != nil {
			return expect.ImportResults{{Path: name, Err: fmt.Errorf("creating imports cluster: %w", err)}}
		}
		if !o.KeepCluster {
			defer func() {
				fmt.Printf("→ Deleting imports cluster '%s'...\n", name)
				_ = cluster.DeleteKind(ctx, name, cluster.KindOptions{BinDir: o.KindBinDir})
			}()
		}
		sharedCtx, useCurrent = "kind-"+name, false
	}
	if !o.Keep {
		defer func() {
			post.Teardown(context.Background(), parentUndos)
		}()
	}

	// The ecosystem the imports on a shared cluster share: this test's, when
	// they share its cluster, else the first import's to declare one.
	eco := o.suiteEcosystem
	if eco == nil {
		eco = &suiteEcosystem{installedOn: map[string]bool{}}
		if r.ecosystem != nil {
			eco.declared, eco.plan = r.spec.Environment.Ecosystem, r.ecosystem
			if r.needsCluster {
				eco.installedOn[r.effectiveContext(ctx)] = true
			}
		}
	}

	var results expect.ImportResults
	for _, imp := range s.Imports {
		ir := expect.ImportResult{Path: imp.Path}
		if imp.Wait != "" {
			d, _ := core.ParseTimeDuration(imp.Wait) // validated when the test was read
			fmt.Printf("→ Waiting %s before %s...\n", imp.Wait, filepath.Base(imp.Path))
			time.Sleep(d)
		}
		sub, err := o.Load(r.abs(imp.Path))
		if err != nil {
			ir.Err = fmt.Errorf("loading import %s: %w", imp.Path, err)
			results = append(results, ir)
			continue
		}
		sub.Env = sub.Env.Inherit(s.Env)

		so := o
		so.suppressReport, so.SpecPath = true, imp.Path
		so.shared, so.parentUndos, so.suiteEcosystem = false, nil, nil
		switch {
		case imp.FreshCluster:
			so.Context, so.UseCurrent = "", false
		case imp.Context != "":
			// Its own cluster, which jig neither creates nor deletes: it
			// undoes what it did there, shares nothing with the parent.
			so.Context, so.UseCurrent = imp.Context, false
		default:
			so.Context, so.UseCurrent = sharedCtx, useCurrent
			so.shared, so.parentUndos = true, &parentUndos
			so.suiteEcosystem = eco
		}
		res, err := Run(ctx, sub, so)
		ir.Result, ir.Err = res, err
		results = append(results, ir)
	}
	return results
}

// hasSharedImports reports whether any import shares the parent's
// cluster: not a fresh one, not another context.
func (r *runner) hasSharedImports() bool {
	for _, imp := range r.spec.Imports {
		if !imp.FreshCluster && imp.Context == "" {
			return true
		}
	}
	return false
}
