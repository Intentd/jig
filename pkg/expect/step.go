package expect

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/assert"
	"github.com/intentd/tools/command"
	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/resolver/env"
	"strings"
	"time"

	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/tools/resolver"
	"k8s.io/client-go/kubernetes"
)

// errSkipped is a sentinel returned when a when:/or: gate is not met.
// The runner detects it to record a skipped case rather than a failure.
var errSkipped = fmt.Errorf("skipped")

// StepResult is what one expectation's run produced: the outcome plus
// how many attempts it took and how many were configured. The runner copies
// these into the case result for the report.
type StepResult struct {
	Attempts   int  // attempts actually run
	Configured int  // 1 + retry count; 1 when no retry is configured
	Skipped    bool // when:/or: gate not met; the expectation did not run
}

// runStep runs an expectation, retrying up to exp.Retry times when it fails.
// Each attempt gets its own timeout; the retry re-runs the same operations,
// so an idempotent step (apply, migrate, get) recovers on the next attempt.
// It returns the outcome, the attempt count, and the error: errSkipped when
// the when:/or: gate is not met, nil on pass, and the last attempt's error on
// failure. Configured is the expectation's own setting, so a skipped one
// reports it too, with no attempts.
func runStep(ctx context.Context, sc spec.ExpectationScope, resolver *resolver.Resolver) (StepResult, error) {
	exp := sc.Expectation
	attempts := 1 + max(exp.Retry, 0)
	res := StepResult{Configured: attempts}

	if !condition.Evaluate(sc.CRData, exp.When, exp.Or, resolver.Evaluator()) {
		res.Skipped = true
		return res, errSkipped
	}

	// A server dry run sends the writes once: nothing is persisted, so there
	// is nothing to poll for or retry.
	run := runAttempt
	if kubectl.DryRun() {
		run, attempts = dryRunAttempt, 1
	}

	var lastErr error
	for n := 1; n <= attempts; n++ {
		lastErr = run(ctx, sc, resolver)
		res.Attempts = n
		if lastErr == nil {
			return res, nil
		}
	}
	return res, lastErr
}

// dryRunAttempt is an attempt under --dry-run=server: the expectation's kubectl
// writes go to the API server, which checks them (schema, admission, RBAC)
// without persisting anything. Commands, resource checks, external calls and
// kubectl assertions are skipped: they would read state the writes never
// created, and commands are shell the dry run cannot reach.
func dryRunAttempt(ctx context.Context, sc spec.ExpectationScope, _ *resolver.Resolver) error {
	k := sc.Expectation.Kubectl
	if k == nil {
		return nil
	}
	return kubectl.Write(ctx, kubectl.BlockOptions{
		Kubectl: k,
		WorkDir: sc.WorkDir,
		Env:     sc.Env,
		Client:  sc.CS,
	})
}

// runAttempt runs one attempt; tests replace it to script attempt outcomes.
var runAttempt = attempt

// pollInterval is how often an attempt re-runs its checks until its timeout.
var pollInterval = 3 * time.Second

// attempt is one attempt: wait, then poll runOperations until it passes or
// the expectation's timeout expires; the last poll lands on the deadline. It
// resets the wrote flag, so a retry re-runs the kubectl writes (apply, patch,
// restart, scale, delete) — safe because the operations are idempotent.
func attempt(ctx context.Context, sc spec.ExpectationScope, resolver *resolver.Resolver) error {
	exp := sc.Expectation

	if exp.Wait != "" {
		if d, err := parseTimeDuration(exp.Wait); err == nil && d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	timeout, err := parseTimeDuration(exp.Timeout)
	if err != nil || timeout <= 0 {
		timeout = 60 * time.Second
	}

	deadline := time.Now().Add(timeout)

	// kubectl writes run until they succeed once; only the assertions are
	// retried after that. Re-running a write on every poll iteration would
	// undo what the assertion waits for, e.g. a drift patch re-applied right
	// before checking it was reverted.
	wrote := exp.Kubectl == nil
	sc.Wrote = &wrote

	ops := stepOperations(exp)
	var seen string
	for {
		err := runOperations(ctx, ops, sc, resolver)
		if err == nil {
			return holds(ctx, exp, ops, sc, resolver)
		}
		// A poll's error is shown when it differs from the one before, so a
		// failure that changes the cluster and then gives way to another is
		// not hidden behind the last one.
		if msg := err.Error(); msg != seen {
			seen = msg
			fmt.Printf("    not yet: %s\n", strings.ReplaceAll(msg, "\n", "\n      "))
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			// The last poll's error: checking again would run the commands
			// once more, and could pass after the attempt has failed.
			return fmt.Errorf("timeout after %s waiting for %q: %w", timeout, exp.Name, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(remaining, pollInterval)):
		}
	}
}

// operations is what a step or a post block runs, in order. Extracting them
// keeps runOperations independent of the expectation's other fields (Timeout,
// Retry, Finally, …) and makes it usable from a test with only the lists. A
// post block has no resources: they are assertions only.
type operations struct {
	Commands  []spec.Command
	Kubectl   *spec.Kubectl
	Resources []spec.ResourceCheck
	External  []spec.ExternalCheck
	Order     []spec.OrderEntry
}

func stepOperations(exp spec.Expectation) operations {
	return operations{exp.Commands, exp.Kubectl, exp.Resources, exp.External, exp.Order}
}

// holds keeps polling the checks that have just passed for the
// expectation's stays window: the first failure in it fails the
// expectation, naming how long they held. Without stays it returns nil.
func holds(ctx context.Context, exp spec.Expectation, ops operations, sc spec.ExpectationScope, resolver *resolver.Resolver) error {
	if exp.Stays == "" {
		return nil
	}
	window, err := time.ParseDuration(exp.Stays)
	if err != nil {
		return fmt.Errorf("stays %q: %w", exp.Stays, err)
	}
	start := time.Now()
	for time.Since(start) < window {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(pollInterval, window-time.Since(start))):
		}
		if err := runOperations(ctx, ops, sc, resolver); err != nil {
			return fmt.Errorf("held for %s of %s, then: %w", time.Since(start).Round(time.Second), window, err)
		}
	}
	return nil
}

// runOperations runs a step's operations once, stopping at the first that
// fails: commands, then kubectl (writes once, then assertions), then
// resources, then external calls.
func runOperations(ctx context.Context, ops operations, sc spec.ExpectationScope, resolver *resolver.Resolver) error {
	// Commands run before resources so that action commands (e.g. cleanup deletes)
	// execute before the resource state is checked.
	for _, cmd := range ops.Commands {
		out, err := runCommand(ctx, cmd, sc.WorkDir, sc.Env)
		if err != nil {
			return err
		}
		if sc.Capture != nil {
			if c := cmd.JSON.Captured(out); c != nil {
				sc.Capture(c)
			}
		}
	}
	if ops.Kubectl != nil {
		if !*sc.Wrote {
			if err := kubectl.Write(ctx, kubectl.BlockOptions{
				Kubectl: ops.Kubectl,
				WorkDir: sc.WorkDir,
				Env:     sc.Env,
				Client:  sc.CS,
			}); err != nil {
				return err
			}
			*sc.Wrote = true
		}
		if err := kubectl.Assert(ctx, kubectl.BlockOptions{
			Kubectl: ops.Kubectl,
			Env:     sc.Env,
			WorkDir: sc.WorkDir,
			Client:  sc.CS,
			Config:  sc.Cfg,
			Dynamic: sc.Dynamic,
			Mapper:  sc.Mapper,
			Capture: sc.Capture,
		}); err != nil {
			return err
		}
	}
	for _, r := range ops.Resources {
		if err := assertResource(ctx, r, sc.Env, sc.WorkDir); err != nil {
			return err
		}
	}
	for _, c := range ops.External {
		if _, err := runExternal(ctx, c, sc.WorkDir, resolver, sc.CS); err != nil {
			return err
		}
	}
	if len(ops.Order) > 0 {
		if err := checkOrder(ctx, ops.Order, sc); err != nil {
			return err
		}
	}
	return nil
}

// runExternal makes the call and asserts on its result, returning what the
// assertions saw. A call that fails fails the operation, as does an assertion
// that does not hold.
func runExternal(ctx context.Context, c spec.ExternalCheck, workDir string, resolver *resolver.Resolver, cs kubernetes.Interface) (string, error) {
	if c.Repeat > 1 {
		return runExternalRepeat(ctx, c, resolver, cs)
	}
	result, err := spec.CallExternal(ctx, "external", resolver, c.ExternalCall, cs)
	if err != nil {
		return "", fmt.Errorf("external %q: %w", c.Name, err)
	}
	if msg, _ := result["error"].(string); msg != "" {
		return "", fmt.Errorf("external %q: %s", c.Name, msg)
	}
	// A call whose when:/or: does not pass is skipped, like a gated step.
	if called, _ := result["called"].(string); called == "false" {
		return "", nil
	}
	out, err := externalOutput(ctx, result, assert.Extractor{JSONPath: c.JSONPath, JQ: c.JQ, YQ: c.YQ}, workDir)
	if err != nil {
		return "", fmt.Errorf("external %q: %w", c.Name, err)
	}
	if c.Assert != nil {
		if err := c.Assert.Check(out); err != nil {
			return out, fmt.Errorf("external %q: %w\noutput: %s", c.Name, err, out)
		}
	}
	return out, nil
}

// externalOutput is what a check's assertions apply to: the extractor over
// the result when set, else the body for HTTP and result for the other protocols.
func externalOutput(ctx context.Context, result map[string]interface{}, ext assert.Extractor, workDir string) (string, error) {
	// What the assertions see by default: body for HTTP, result otherwise.
	defaultOut := fmt.Sprint(result["result"])
	if body, ok := result["body"].(string); ok {
		defaultOut = body
	}

	switch {
	case ext.JSONPath != "":
		// Navigate the default output as JSON (the body for HTTP).
		return applyExtract(ctx, assert.ExtractRequest{
			WorkDir: workDir, Output: defaultOut,
			Extractor: assert.Extractor{JSONPath: ext.JSONPath},
		})
	case ext.JQ != "" || ext.YQ != "":
		// External extractors see the whole envelope.
		raw, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		return applyExtract(ctx, assert.ExtractRequest{
			WorkDir: workDir, Output: string(raw),
			Extractor: assert.Extractor{JQ: ext.JQ, YQ: ext.YQ},
		})
	default:
		return defaultOut, nil
	}
}

// assertResource asserts the state of any Kubernetes resource using kubectl.
// Kind can be any built-in or custom resource kind.
func assertResource(ctx context.Context, r spec.ResourceCheck, env env.Resolver, workDir string) error {
	// count=0: assert resource(s) do NOT exist.
	// A kubectl error (CRD unknown, type not registered) also means nothing exists — pass.
	if r.Count != nil && *r.Count == 0 {
		out, err := kubectl.Get(ctx, kubectl.GetOptions{
			Kind:           r.Kind,
			Name:           r.Name,
			Namespace:      r.Namespace,
			IgnoreNotFound: true,
			Format:         "name",
			WorkDir:        workDir,
			Env:            env,
		})
		if err != nil {
			return nil // kubectl error = CRD unknown or type missing = nothing exists
		}
		if strings.TrimSpace(out) != "" {
			if r.Name != "" {
				return fmt.Errorf("%s/%s still exists in %s", r.Kind, r.Name, r.Namespace)
			}
			return fmt.Errorf("%s still exists in namespace %s:\n%s", r.Kind, r.Namespace, out)
		}
		return nil
	}

	// Named resource: assert it exists.
	if r.Name != "" {
		out, err := kubectl.Get(ctx, kubectl.GetOptions{
			Kind:           r.Kind,
			Name:           r.Name,
			Namespace:      r.Namespace,
			IgnoreNotFound: true,
			Format:         "name",
			WorkDir:        workDir,
			Env:            env,
		})
		if err != nil || strings.TrimSpace(out) == "" {
			return fmt.Errorf("%s/%s not found in %s", r.Kind, r.Name, r.Namespace)
		}
		if r.Ready {
			return assertReady(ctx, env, workDir, r.Kind, r.Name, r.Namespace)
		}
		return nil
	}

	// Unnamed: assert at least one (or exact count) exists.
	out, err := kubectl.Get(ctx, kubectl.GetOptions{
		Kind:      r.Kind,
		Namespace: r.Namespace,
		Format:    "name",
		WorkDir:   workDir,
		Env:       env,
	})
	if err != nil || strings.TrimSpace(out) == "" {
		return fmt.Errorf("no %s found in namespace %s", r.Kind, r.Namespace)
	}
	if r.Count != nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != *r.Count {
			return fmt.Errorf("%s count in %s: want %d, got %d", r.Kind, r.Namespace, *r.Count, len(lines))
		}
	}
	if r.Ready {
		first := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
		if parts := strings.SplitN(first, "/", 2); len(parts) == 2 {
			first = parts[1]
		}
		return assertReady(ctx, env, workDir, r.Kind, first, r.Namespace)
	}
	return nil
}

// assertReady checks whether a resource has available replicas.
// Uses jsonpath to read status.availableReplicas — covers Deployments and
// StatefulSets. Returns nil (ready) if the field is absent (e.g. Services).
func assertReady(ctx context.Context, env env.Resolver, workDir, kind, name, ns string) error {
	out, err := kubectl.Get(ctx, kubectl.GetOptions{
		Kind:      kind,
		Name:      name,
		Namespace: ns,
		Format:    "jsonpath={.status.availableReplicas}",
		WorkDir:   workDir,
		Env:       env,
	})
	if err != nil {
		return fmt.Errorf("%s/%s in %s: not ready (%s)", kind, name, ns, strings.TrimSpace(out))
	}
	val := strings.TrimSpace(out)
	if val == "0" {
		return fmt.Errorf("%s/%s in %s: not ready (availableReplicas=0)", kind, name, ns)
	}
	// val == "" means field doesn't exist on this kind — treat as ready (e.g. Service).
	return nil
}

// runCommand runs c.Run with sh -c in workDir, with the expectation's env in
// the process environment, and asserts on its exit code and output. It
// returns what the assertions saw: the output, extracted when c declares an
// extractor.
func runCommand(ctx context.Context, c spec.Command, workDir string, resolver env.Resolver) (string, error) {
	res, err := command.Run(ctx, command.Command{Shell: c.Run, Dir: workDir, Env: resolver.Vars()})
	if err != nil {
		return "", fmt.Errorf("running %q: %w", c.Run, err)
	}
	out, exitCode := res.Output, res.ExitCode

	if exitCode != c.ExitCode {
		return out, fmt.Errorf("command %q: expected exit code %d, got %d\noutput: %s",
			c.Run, c.ExitCode, exitCode, strings.TrimSpace(out))
	}

	output, err := applyExtract(ctx, assert.ExtractRequest{
		WorkDir:   workDir,
		Output:    out,
		Extractor: assert.Extractor{JSONPath: c.JSONPath, JQ: c.JQ, YQ: c.YQ},
	})
	if err != nil {
		return out, fmt.Errorf("command %q: %w", c.Run, err)
	}

	if inline := (assert.Assertions{TextAssertion: c.TextAssertion}); !inline.Empty() {
		if err := inline.Check(output); err != nil {
			return output, fmt.Errorf("command %q: %w\noutput: %s", c.Run, err, strings.TrimSpace(out))
		}
	}
	if len(c.JSON) > 0 {
		if err := (assert.Assertions{JSON: c.JSON}).Check(output); err != nil {
			return output, fmt.Errorf("command %q: %w\noutput: %s", c.Run, err, strings.TrimSpace(out))
		}
	}
	if c.Assert != nil {
		if err := c.Assert.Check(output); err != nil {
			return output, fmt.Errorf("command %q: %w\noutput: %s", c.Run, err, strings.TrimSpace(out))
		}
	}
	return output, nil
}
