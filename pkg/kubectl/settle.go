package kubectl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/resolver/env"
)

// settlePoll is how often a settling object's resourceVersion is read.
var settlePoll = 2 * time.Second

// resourceVersion reads e's object's resourceVersion; tests replace it.
var resourceVersion = func(ctx context.Context, e spec.KubectlGet, vars env.Resolver, workDir string) (string, error) {
	return Get(ctx, GetOptions{
		Kind: e.Kind, Name: e.Name, Namespace: e.Namespace,
		Format: "jsonpath={.metadata.resourceVersion}", WorkDir: workDir, Env: vars,
	})
}

// waitSettled waits until e's object keeps one resourceVersion for its
// settles window: it has stopped changing. It gives up after four windows
// and a minute more, naming how often the object changed.
func waitSettled(ctx context.Context, e spec.KubectlGet, vars env.Resolver, workDir string) error {
	window, err := time.ParseDuration(e.Settles)
	if err != nil {
		return fmt.Errorf("settles %q: %w", e.Settles, err)
	}
	giveUp := time.Now().Add(4*window + time.Minute)
	last, since, changes := "", time.Now(), 0
	for {
		rv, err := resourceVersion(ctx, e, vars, workDir)
		if err != nil {
			return fmt.Errorf("settles: %w", err)
		}
		rv = strings.TrimSpace(rv)
		if rv != last {
			if last != "" {
				changes++
			}
			last, since = rv, time.Now()
		}
		if time.Since(since) >= window {
			return nil
		}
		if time.Now().After(giveUp) {
			return fmt.Errorf("%s/%s did not settle: its resourceVersion changed %d time(s), never staying %s", e.Kind, e.Name, changes, window)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(settlePoll):
		}
	}
}
