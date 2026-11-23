package kubectl

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/resolver/env"
)

// An object whose resourceVersion stops changing settles; one that keeps
// changing doesn't, and the error says how often it changed.
func TestWaitSettled(t *testing.T) {
	oldPoll, oldRV := settlePoll, resourceVersion
	settlePoll = 10 * time.Millisecond
	t.Cleanup(func() { settlePoll, resourceVersion = oldPoll, oldRV })

	reads := 0
	resourceVersion = func(context.Context, spec.KubectlGet, env.Resolver, string) (string, error) {
		reads++
		return fmt.Sprint(min(reads, 3)), nil // changes twice, then stays
	}
	assert.NoError(t, waitSettled(context.Background(), spec.KubectlGet{Kind: "Deployment", Name: "api", Settles: "50ms"}, env.Resolver{}, ""))

	reads = 0
	resourceVersion = func(context.Context, spec.KubectlGet, env.Resolver, string) (string, error) {
		reads++
		return fmt.Sprint(reads), nil // never the same twice
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := waitSettled(ctx, spec.KubectlGet{Kind: "Deployment", Name: "api", Settles: "50ms"}, env.Resolver{}, "")
	assert.Error(t, err, "a version that keeps changing never settles")
}
