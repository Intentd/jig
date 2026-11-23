package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/jig/pkg/post"
	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/resolver/env"
)

func load(t *testing.T, doc string) *spec.Declaration {
	t.Helper()
	p := filepath.Join(t.TempDir(), "jig.yaml")
	require.NoError(t, os.WriteFile(p, []byte(doc), 0o644))
	s, err := spec.FromFile(p)
	require.NoError(t, err)
	return s
}

// Steps run in order; each expectation runs after the environment, after
// its named step, or after the last step.
func TestRun_OrdersStepsAndExpectations(t *testing.T) {
	s := load(t, `name: order
environment:
  cluster: { provider: none }
prepare:
  - name: one
    commands: [{ run: "echo one >> log" }]
  - name: two
    commands: [{ run: "echo two >> log" }]
expect:
  - name: before any step
    after: environment
    commands: [{ run: "test ! -e log" }]
  - name: after one
    after: one
    commands: [{ run: "cat log", equals: "one" }]
  - name: after all
    commands: [{ run: "tr '\n' ' ' < log", contains: "one two" }]
`)
	res, err := Run(context.Background(), s, Options{})
	require.NoError(t, err)
	require.Len(t, res.Cases, 3)
	for _, c := range res.Cases {
		assert.True(t, c.Passed, "%s: %v", c.Name, c.Err)
	}
	assert.Equal(t, []string{"before any step", "after one", "after all"},
		[]string{res.Cases[0].Name, res.Cases[1].Name, res.Cases[2].Name})
}

func TestRun_FailureAndStop(t *testing.T) {
	s := load(t, `name: fails
environment:
  cluster: { provider: none }
prepare:
  - name: one
    commands: [{ run: "true" }]
expect:
  - name: broken
    after: one
    timeout: 1s
    onFailure: { stop: true }
    commands: [{ run: "echo no", equals: "yes" }]
  - name: never
    commands: [{ run: "true" }]
`)
	res, err := Run(context.Background(), s, Options{})
	require.Error(t, err)
	require.Len(t, res.Cases, 1, "stop ends the run at the failure")
	assert.False(t, res.Cases[0].Passed)
	assert.Equal(t, "step", res.StopReason)
}

func TestRun_FailingStepStopsTheRun(t *testing.T) {
	s := load(t, `name: step fails
environment:
  cluster: { provider: none }
prepare:
  - name: bad
    commands: [{ run: "exit 3" }]
expect:
  - name: never
    commands: [{ run: "true" }]
`)
	reportFile := filepath.Join(t.TempDir(), "report.json")
	res, err := Run(context.Background(), s, Options{ReportFile: reportFile})
	assert.ErrorContains(t, err, "exit code 3, want 0")

	// The run still says where it stopped and why, in the result and the
	// report.
	require.NotNil(t, res)
	require.NotNil(t, res.SetupFailure)
	assert.Equal(t, "bad", res.SetupFailure.At)
	assert.False(t, res.AllPassed())
	raw, err := os.ReadFile(reportFile)
	require.NoError(t, err, "a failed step still writes the report")
	assert.Contains(t, string(raw), `"at": "bad"`)
}

func TestRun_ImportsShareEnv(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "child.yaml"), []byte(`name: child
environment:
  cluster: { provider: none }
expect:
  - name: sees the parent's env
    commands: [{ run: "echo $GREETING", equals: "hello" }]
`), 0o644))
	p := filepath.Join(dir, "jig.yaml")
	require.NoError(t, os.WriteFile(p, []byte(`name: parent
env: { GREETING: hello }
environment:
  cluster: { provider: none }
imports: [./child.yaml]
`), 0o644))
	s, err := spec.FromFile(p)
	require.NoError(t, err)
	res, err := Run(context.Background(), s, Options{})
	require.NoError(t, err)
	require.Len(t, res.Imports, 1)
	require.NoError(t, res.Imports[0].Err)
	assert.True(t, res.Imports[0].Result.AllPassed())
}

// A release later imports upgrade is handed to the parent once, at its
// latest upgrade, so the parent's teardown (newest first) removes it before
// the manifests the imports applied ahead of it.
func TestToParent_ReleaseAtItsLatestUpgrade(t *testing.T) {
	var parent []post.Undo
	r := &runner{o: Options{parentUndos: &parent}}
	rel := func() *cluster.HelmRelease { return &cluster.HelmRelease{Release: "inrun", Namespace: "inrun-system"} }

	r.toParent(post.Undo{Label: "bundle1", Path: "b1"})
	r.toParent(post.Undo{Label: "inrun", Release: rel()})
	r.toParent(post.Undo{Label: "bundle2", Path: "b2"})
	r.toParent(post.Undo{Label: "inrun", Release: rel()})

	var labels []string
	for _, u := range parent {
		labels = append(labels, u.Label)
	}
	assert.Equal(t, []string{"bundle1", "bundle2", "inrun"}, labels)
}

// deploy.env and envFrom expand through the run's env and contexts; an
// unknown reference fails naming the image.
func TestExpandDeploy(t *testing.T) {
	vars := spec.Contexts{spec.EcosystemContext: {"postgres.url": "postgresql://app:pw@postgres-rw.data:5432/app"}}.
		Apply(env.New(map[string]string{"NS": "shop"}))
	img := cluster.Image{Name: "api", Image: "api:dev", Deploy: &cluster.ImageDeploy{Ports: cluster.Ports{{Port: 8090, Host: 8090}},
		Env:     map[string]string{"DATABASE_URL": "${{ ecosystem.postgres.url }}", "AUTH_URL": "http://auth.${NS}:8080"},
		EnvFrom: []string{"${NS}-deps"},
	}}
	got, err := expandDeploy(vars, img)
	if err != nil {
		t.Fatal(err)
	}
	if got.Deploy.Env["DATABASE_URL"] != "postgresql://app:pw@postgres-rw.data:5432/app" || got.Deploy.Env["AUTH_URL"] != "http://auth.shop:8080" {
		t.Fatalf("env: %v", got.Deploy.Env)
	}
	if got.Deploy.EnvFrom[0] != "shop-deps" {
		t.Fatalf("envFrom: %v", got.Deploy.EnvFrom)
	}
	if img.Deploy.Env["AUTH_URL"] != "http://auth.${NS}:8080" {
		t.Fatal("the declared image changed")
	}

	img.Deploy.Env = map[string]string{"X": "${{ ecosystem.postgres.uri }}"}
	if _, err := expandDeploy(vars, img); err == nil || !strings.Contains(err.Error(), "image api: deploy: unknown ${{ ecosystem.postgres.uri }}") {
		t.Fatalf("got %v", err)
	}
}
