package runner

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/intentd/jig/pkg/expect"
	"github.com/intentd/jig/pkg/spec"
)

// runGuards checks the jig's guards once, at the end of the run, over
// what happened since it started, and records each like an expectation.
func (r *runner) runGuards(ctx context.Context, exp *expect.Runner, since time.Time) {
	if len(r.spec.Guards) == 0 || r.cs == nil {
		return
	}
	fmt.Println("\n→ Guards")
	for _, g := range r.spec.Guards {
		start := time.Now()
		err := r.guard(ctx, g, since)
		c := expect.CaseResult{Name: "guard: " + g.Name, Passed: err == nil, Err: err, Elapsed: time.Since(start)}
		if err != nil {
			fmt.Printf("  %s %s: %v\n", failureMark(), c.Name, err)
		} else {
			fmt.Printf("  %s %s\n", successMark(), c.Name)
		}
		exp.Record(c)
	}
}

// guard checks one guard.
func (r *runner) guard(ctx context.Context, g spec.Guard, since time.Time) error {
	switch {
	case g.Events != nil:
		return r.guardEvents(ctx, *g.Events, since)
	case g.Restarts != nil:
		return r.guardRestarts(ctx, *g.Restarts)
	case g.Logs != nil:
		return r.guardLogs(ctx, *g.Logs, since)
	}
	return nil
}

// guardEvents fails on events of the type in the namespaces since the run
// started, other than the ignored reasons.
func (r *runner) guardEvents(ctx context.Context, g spec.GuardEvents, since time.Time) error {
	typ := g.Type
	if typ == "" {
		typ = corev1.EventTypeWarning
	}
	var found []string
	for _, ns := range g.Namespaces {
		list, err := r.cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{FieldSelector: "type=" + typ})
		if err != nil {
			return fmt.Errorf("events in %s: %w", ns, err)
		}
		for _, e := range list.Items {
			if slices.Contains(g.Ignore, e.Reason) || eventTime(e).Before(since) {
				continue
			}
			found = append(found, fmt.Sprintf("%s/%s %s: %s", ns, e.InvolvedObject.Name, e.Reason, strings.TrimSpace(e.Message)))
		}
	}
	if len(found) > 0 {
		return fmt.Errorf("%d %s event(s): %s", len(found), typ, strings.Join(found, "; "))
	}
	return nil
}

// eventTime is when an event last happened.
func eventTime(e corev1.Event) time.Time {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}

// guardRestarts fails on any container restart in the pods selected.
func (r *runner) guardRestarts(ctx context.Context, g spec.GuardRestarts) error {
	var found []string
	for _, ns := range g.Namespaces {
		pods, err := r.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: g.LabelSelector})
		if err != nil {
			return fmt.Errorf("pods in %s: %w", ns, err)
		}
		for _, p := range pods.Items {
			for _, c := range p.Status.ContainerStatuses {
				if c.RestartCount > 0 {
					found = append(found, fmt.Sprintf("%s/%s %s ×%d", ns, p.Name, c.Name, c.RestartCount))
				}
			}
		}
	}
	if len(found) > 0 {
		return fmt.Errorf("restarted: %s", strings.Join(found, ", "))
	}
	return nil
}

// guardLogs fails when the logs of the pods selected, since the run
// started, contain the text.
func (r *runner) guardLogs(ctx context.Context, g spec.GuardLogs, since time.Time) error {
	ns := g.Namespace
	if ns == "" {
		ns = "default"
	}
	pods, err := r.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: g.LabelSelector})
	if err != nil {
		return fmt.Errorf("pods in %s: %w", ns, err)
	}
	t := metav1.NewTime(since)
	var found []string
	for _, p := range pods.Items {
		stream, err := r.cs.CoreV1().Pods(ns).GetLogs(p.Name, &corev1.PodLogOptions{Container: g.Container, SinceTime: &t}).Stream(ctx)
		if err != nil {
			continue // a pod without logs yet has nothing to find
		}
		b, _ := io.ReadAll(stream)
		stream.Close()
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, g.NotContains) {
				found = append(found, p.Name+": "+strings.TrimSpace(line))
				break
			}
		}
	}
	if len(found) > 0 {
		return fmt.Errorf("%q in the logs: %s", g.NotContains, strings.Join(found, "; "))
	}
	return nil
}
