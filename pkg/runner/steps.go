package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/jig/pkg/post"
	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/command"
	"github.com/intentd/tools/resolver/env"
	"k8s.io/utils/ptr"
)

// provision runs the environment's manifests, charts and waits, in that
// order, recording each for teardown.
func (r *runner) provision(ctx context.Context, phase string, p spec.Provision) error {
	vars := r.vars()
	for _, e := range p.Apply {
		if err := r.apply(ctx, phase, e, vars); err != nil {
			return err
		}
	}
	for _, h := range p.Helm {
		if err := r.helm(ctx, phase, h, false); err != nil {
			return err
		}
	}
	for _, w := range p.Wait {
		if err := runWait(ctx, w); err != nil {
			return fmt.Errorf("%s wait: %w", phase, err)
		}
	}
	return nil
}

// setupEnvironment is where a run stops when its environment fails.
const setupEnvironment = "environment"

// stepLabel names step i: its name, else prepare[i].
func stepLabel(i int, st spec.Step) string {
	if st.Name != "" {
		return st.Name
	}
	return fmt.Sprintf("prepare[%d]", i)
}

// step runs one prepare step's action, then its waits and restarts.
func (r *runner) step(ctx context.Context, i int, st spec.Step) error {
	label := stepLabel(i, st)
	vars := r.vars()
	keep := r.o.shared && st.Keep
	for _, e := range st.Apply {
		if err := r.applyKept(ctx, label, e, vars, keep); err != nil {
			return err
		}
	}
	if st.Helm != nil {
		if err := r.helm(ctx, label, *st.Helm, keep); err != nil {
			return err
		}
	}
	// A delete doesn't wait for finalizers: the expectations after the step
	// wait for what the deletion brings about, each within its own timeout.
	for _, p := range st.Delete {
		sp := startSpinner(fmt.Sprintf("%s: deleting %s...", label, p))
		if out, err := kubectl.Delete(ctx, kubectl.DeleteOptions{File: r.abs(p), Env: vars, IgnoreNotFound: true, Wait: ptr.To(false)}); err != nil {
			sp.FailureWithUpdate(fmt.Sprintf("%s: could not delete %s", label, p))
			return fmt.Errorf("%s: delete %s: %w\n%s", label, p, err, out)
		}
		sp.SuccessWithUpdate(fmt.Sprintf("%s: deleted %s", label, p))
	}
	for _, c := range st.Commands {
		if err := r.command(ctx, label, c, vars); err != nil {
			return err
		}
	}
	for _, w := range st.Wait {
		if err := runWait(ctx, w); err != nil {
			return fmt.Errorf("%s: wait: %w", label, err)
		}
	}
	for _, rs := range st.Restart {
		target := strings.ToLower(rs.Kind) + "/" + rs.Name
		sp := startSpinner(fmt.Sprintf("Restarting %s...", target))
		if out, err := kubectl.Restart(ctx, kubectl.RestartOptions{Target: target, Namespace: rs.Namespace}); err != nil {
			sp.FailureWithUpdate(fmt.Sprintf("Could not restart %s", target))
			return fmt.Errorf("%s: restart %s: %w\n%s", label, target, err, out)
		}
		if out, err := kubectl.Rollout(ctx, kubectl.RolloutOptions{Sub: kubectl.RolloutStatus, Target: target, Namespace: rs.Namespace}); err != nil {
			sp.FailureWithUpdate(fmt.Sprintf("%s did not roll out", target))
			return fmt.Errorf("%s: restart %s: %w\n%s", label, target, err, out)
		}
		sp.SuccessWithUpdate(fmt.Sprintf("Restarted %s", target))
	}
	return nil
}

// apply applies one manifest and its waits, recording it for teardown.
func (r *runner) apply(ctx context.Context, label string, e spec.ApplyEntry, vars env.Resolver) error {
	return r.applyKept(ctx, label, e, vars, false)
}

// applyKept is apply; with keep, the parent undoes the manifest, after its
// last import.
func (r *runner) applyKept(ctx context.Context, label string, e spec.ApplyEntry, vars env.Resolver, keep bool) error {
	path := r.abs(e.Path)
	sp := startSpinner(fmt.Sprintf("%s: applying %s...", label, e.Path))
	if out, err := kubectl.Apply(ctx, kubectl.ApplyOptions{File: path, Env: vars}); err != nil {
		sp.FailureWithUpdate(fmt.Sprintf("%s: could not apply %s", label, e.Path))
		return fmt.Errorf("%s: apply %s: %w\n%s", label, e.Path, err, out)
	}
	sp.SuccessWithUpdate(fmt.Sprintf("%s: applied %s", label, e.Path))
	u := post.Undo{Label: label + " " + filepath.Base(path), Path: path}
	if keep {
		r.toParent(u)
		u.Keep = true
	}
	r.undos = append(r.undos, u)
	for _, w := range e.Wait {
		if err := runWait(ctx, w); err != nil {
			return fmt.Errorf("%s: apply %s: wait: %w", label, e.Path, err)
		}
	}
	return nil
}

// helm installs or upgrades one chart and runs its waits. Its release is
// recorded for teardown; with keep, the parent uninstalls it after its last
// import instead.
func (r *runner) helm(ctx context.Context, label string, h spec.HelmInstall, keep bool) error {
	if h.IsLocalChart() {
		h.Chart = r.abs(h.Chart)
	}
	files := make([]string, len(h.ValueFiles))
	for i, f := range h.ValueFiles {
		files[i] = r.abs(f)
	}
	h.ValueFiles = files
	rel := h.HelmRelease()
	sp := startSpinner(fmt.Sprintf("%s: helm %s...", label, h.ReleaseName()))
	if err := cluster.HelmInstall(ctx, rel); err != nil {
		sp.FailureWithUpdate(fmt.Sprintf("%s: helm %s failed", label, h.ReleaseName()))
		return fmt.Errorf("%s: helm %s: %w", label, h.Chart, err)
	}
	sp.SuccessWithUpdate(fmt.Sprintf("%s: helm %s installed", label, h.ReleaseName()))
	u := post.Undo{Label: label + " helm " + h.ReleaseName(), Release: &rel}
	if keep {
		r.toParent(u)
		u.Keep = true
	}
	r.undos = append(r.undos, u)
	for _, w := range h.Wait {
		if err := runWait(ctx, w); err != nil {
			return fmt.Errorf("%s: helm %s: wait: %w", label, h.ReleaseName(), err)
		}
	}
	return nil
}

// toParent hands u to the parent, to undo after its last import, newest
// first. A release later imports upgrade is handed over once, at its latest
// upgrade: it goes before the manifests the imports applied ahead of it.
func (r *runner) toParent(u post.Undo) {
	if r.o.parentUndos == nil {
		return
	}
	undos := *r.o.parentUndos
	if u.Release != nil {
		undos = slices.DeleteFunc(undos, func(p post.Undo) bool {
			return p.Release != nil &&
				p.Release.Name() == u.Release.Name() && p.Release.Namespace == u.Release.Namespace
		})
	}
	*r.o.parentUndos = append(undos, u)
}

// command runs one step command from the jig's directory, with its env;
// it fails when the exit code isn't the expected one (0 by default).
func (r *runner) command(ctx context.Context, label string, c spec.Command, vars env.Resolver) error {
	if err := spec.Unresolved(vars, c); err != nil {
		return fmt.Errorf("%s: %s: %w", label, c.Run, err)
	}
	c = env.ExpandAll(vars, c) // ${{ env.NAME }}; the shell expands the rest
	fmt.Printf("→ %s: %s\n", label, c.Run)
	res, err := command.Run(ctx, command.Command{Shell: c.Run, Dir: r.spec.Dir, Env: vars.Vars()})
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if res.ExitCode != c.ExitCode {
		return fmt.Errorf("%s: %s: exit code %d, want %d\n%s", label, c.Run, res.ExitCode, c.ExitCode, res.Output)
	}
	return nil
}

// vars is the jig's env and the run's contexts, for ${NAME} and
// ${{ ecosystem.… }} in manifests and commands.
func (r *runner) vars() env.Resolver {
	return r.contexts.Apply(spec.Expectation{}.ResolveEnv(r.spec.Env))
}

func runWait(ctx context.Context, w spec.Wait) error {
	loc := w.Kind + " " + w.Name
	if w.Namespace != "" {
		loc += " (" + w.Namespace + ")"
	}
	sp := startSpinner(fmt.Sprintf("Waiting for %s...", loc))
	if err := cluster.WaitForResource(ctx, cluster.Wait{Kind: w.Kind, Name: w.Name, Namespace: w.Namespace, Ready: w.Ready, Timeout: w.Timeout}); err != nil {
		sp.FailureWithUpdate(fmt.Sprintf("%s did not become ready", loc))
		return err
	}
	sp.SuccessWithUpdate(fmt.Sprintf("%s ready", loc))
	return nil
}
