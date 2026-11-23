package kubectl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A snapshot leaves out what changes from one run to the next, and its
// diff names each path that differs.
func TestSnapshot_NormalizeAndDiff(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{
			"name": "api", "uid": "u", "resourceVersion": "9", "creationTimestamp": "t",
			"annotations": map[string]any{"kubectl.kubernetes.io/last-applied-configuration": "{}"},
		},
		"spec":   map[string]any{"replicas": 2},
		"status": map[string]any{"readyReplicas": 2},
	}
	n := normalize(obj)
	assert.Equal(t, map[string]any{"metadata": map[string]any{"name": "api"}, "spec": map[string]any{"replicas": 2}}, n)
	assert.Contains(t, obj, "status", "the object itself is untouched")

	want, err := yaml.Marshal(n)
	require.NoError(t, err)
	got, err := yaml.Marshal(map[string]any{"metadata": map[string]any{"name": "api"}, "spec": map[string]any{"replicas": 3, "paused": true}})
	require.NoError(t, err)
	diff, err := snapshotDiff(want, got)
	require.NoError(t, err)
	assert.Contains(t, diff, ".spec.replicas: 2 → 3")
	assert.Contains(t, diff, ".spec.paused: added true")

	same, err := snapshotDiff(want, want)
	require.NoError(t, err)
	assert.Empty(t, same)
}
