package expect

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"text/template"
	"time"

	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/resolver"
	"github.com/intentd/tools/resolver/env"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Options configure a Runner.
type Options struct {
	WorkDir     string
	MaxFailures int

	CS      kubernetes.Interface
	Cfg     *rest.Config
	Dynamic dynamic.Interface
	Mapper  apimeta.RESTMapper

	// Env is the spec's env; each expectation's own env overlays it.
	Env map[string]string
	// Functions are the template functions expectations start from; nil is
	// KEL's. SpecFunctions are added to them.
	Functions     template.FuncMap
	SpecFunctions []resolver.Function
	// OnFailure is the spec's onFailure: its stop applies to every
	// expectation that doesn't set its own.
	OnFailure *spec.OnFailure
	// Data is what when:/or: conditions read, such as the object under test.
	Data map[string]any
	// Contexts are added to every expectation's env: ${{ ecosystem.… }}.
	Contexts spec.Contexts
	// TemplateData is what templates and CEL read besides env, by key, such
	// as the ecosystem's connection details under ecosystem.
	TemplateData map[string]any
}

// Runner runs expectations, group by group, keeping the results, the
// failure count and the stop across groups: a run calls Run once per
// after: group, between its steps.
type Runner struct {
	opts       Options
	resolver   *resolver.Resolver
	cases      []CaseResult
	failures   int
	stopped    error
	stopReason string
	// captured are the values capture: saved, by name, for the
	// expectations after the one that saved them.
	captured map[string]string
}

// Record adds a result made outside the expectations, such as a guard's,
// counting it as a failure when it didn't pass.
func (r *Runner) Record(c CaseResult) {
	r.cases = append(r.cases, c)
	if !c.Passed && !c.Skipped {
		r.failures++
	}
}

// capture saves captured values for the expectations after this one.
func (r *Runner) capture(values map[string]string) {
	if r.captured == nil {
		r.captured = map[string]string{}
	}
	for k, v := range values {
		r.captured[k] = v
	}
}

// withCaptured is the jig's env with the values captured so far over it.
func (r *Runner) withCaptured(env map[string]string) map[string]string {
	if len(r.captured) == 0 {
		return env
	}
	out := make(map[string]string, len(env)+len(r.captured))
	for k, v := range env {
		out[k] = v
	}
	for k, v := range r.captured {
		out[k] = v
	}
	return out
}

// NewRunner returns a Runner for opts.
func NewRunner(opts Options) *Runner {
	return &Runner{opts: opts, resolver: withData(resolver.NewWithFunctions(opts.Functions, opts.SpecFunctions...), opts.TemplateData)}
}

// withData returns r with each of data's keys added, in key order.
func withData(r *resolver.Resolver, data map[string]any) *resolver.Resolver {
	for _, k := range slices.Sorted(maps.Keys(data)) {
		r = r.With(k, data[k])
	}
	return r
}

// Cases are the results so far.
func (r *Runner) Cases() []CaseResult { return r.cases }

// Failures counts the failed expectations so far.
func (r *Runner) Failures() int { return r.failures }

// Stopped reports why the run stopped ("step", "max-failures"), or "" when
// it hasn't.
func (r *Runner) Stopped() (error, string) { return r.stopped, r.stopReason }

const defaultTimeout = "60s"

// Run runs expects in order, recording each result, and reports whether
// the run stopped: an expectation requested it, or the failure count
// reached MaxFailures. After a stop, Run runs nothing.
func (r *Runner) Run(ctx context.Context, expects spec.Expectations) bool {
	if r.stopped != nil {
		return true
	}
	opts := r.opts
	for _, exp := range expects {
		// Resolve the expectation's env once, as the template resolver
		// resolves a source: what runs below sees final values, and a name
		// nothing declares fails the case before it runs.
		vars := opts.Contexts.Apply(exp.ResolveEnv(r.withCaptured(opts.Env)))
		unresolved := spec.Unresolved(vars, exp)
		exp = env.ExpandAll(vars, exp)
		expResolver := r.resolver.WithEnv(vars.Map())
		to := exp.Timeout
		if to == "" {
			to = defaultTimeout
		}
		fmt.Printf("  Waiting for %q (timeout: %s)...\n", exp.Name, to)

		caseStart := time.Now()
		waitDone := make(chan struct{})
		go func() {
			t := time.NewTicker(10 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-waitDone:
					return
				case <-t.C:
					fmt.Printf("    still waiting [%s]...\n", time.Since(caseStart).Round(time.Second))
				}
			}
		}()

		workDir := exp.Dir(opts.WorkDir)

		sc := spec.ExpectationScope{
			Expectation: exp,
			WorkDir:     workDir,
			CS:          opts.CS,
			Cfg:         opts.Cfg,
			Dynamic:     opts.Dynamic,
			Mapper:      opts.Mapper,
			Env:         vars,
			CRData:      opts.Data,
			Capture:     r.capture,
		}
		var vr StepResult
		var verifyErr error
		if unresolved != nil {
			vr = StepResult{Configured: 1 + max(exp.Retry, 0)}
			verifyErr = unresolved
		} else {
			vr, verifyErr = runStep(ctx, sc, expResolver)
		}
		close(waitDone)
		caseElapsed := time.Since(caseStart)

		skipped := verifyErr == errSkipped
		cres := CaseResult{
			Name:     exp.Name,
			Passed:   verifyErr == nil,
			Skipped:  skipped,
			Elapsed:  caseElapsed,
			Err:      verifyErr,
			Attempts: vr.Attempts,
			Retries:  vr.Configured - 1,
		}
		if skipped {
			cres.Err = nil
		}
		r.cases = append(r.cases, cres)

		switch {
		case skipped:
			fmt.Printf("  ~ %s (skipped)\n", exp.Name)

		case verifyErr != nil:
			fmt.Printf("  %s %s (%s): %v\n",
				failureMark(), exp.Name, caseElapsed.Round(time.Millisecond), verifyErr)
			// A dry run changed nothing, so there is nothing to diagnose or
			// clean up; onFailure and finally run shell the dry run cannot reach.
			if !kubectl.DryRun() && exp.OnFailure != nil {
				cres := &r.cases[len(r.cases)-1]
				cres.Warnings = append(cres.Warnings,
					runPost(ctx, "onFailure", &exp.OnFailure.SideEffects, sc, expResolver)...)
			}
			r.failures++

			if exp.EffectiveStop(opts.OnFailure) {
				r.stopped, r.stopReason = errMaxFailures, "step"
			} else if opts.MaxFailures > 0 && r.failures >= opts.MaxFailures {
				r.stopped, r.stopReason = errMaxFailures, "max-failures"
			}

		default:
			fmt.Printf("  %s %s (%s)\n",
				successMark(), exp.Name, caseElapsed.Round(time.Millisecond))
		}

		if !kubectl.DryRun() && exp.Finally != nil {
			cres := &r.cases[len(r.cases)-1]
			cres.Warnings = append(cres.Warnings,
				runPost(ctx, "finally", &exp.Finally.SideEffects, sc, expResolver)...)
		}
		if r.stopped != nil {
			return true
		}
	}

	return r.stopped != nil
}
