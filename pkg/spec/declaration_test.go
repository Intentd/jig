package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/tools/resolver/env"
	"github.com/intentd/tools/structured/structuredtest"
)

const example = `name: shop-api
description: the shop API against its chart
env:
  NS: shop
environment:
  cluster: { name: shop-e2e }
  images: [shop-api:dev]
prepare:
  - name: chart
    helm: { chart: ./charts/shop, namespace: shop }
    wait:
      - { kind: Deployment, name: shop-api, namespace: shop, ready: true }
  - name: order
    apply: ./e2e/order.yaml
    restart:
      - { kind: Deployment, name: shop-api, namespace: shop }
expect:
  - name: infrastructure
    after: environment
    timeout: 30s
  - name: deployed
    after: chart
    timeout: 60s
  - name: the order is accepted
    timeout: 30s
    commands:
      - run: echo accepted
        contains: accepted
`

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "jig.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func TestFromFile(t *testing.T) {
	s, err := FromFile(write(t, example))
	require.NoError(t, err)
	assert.Equal(t, "shop-api", s.Name)
	assert.Equal(t, "shop-e2e", s.Environment.ClusterName("jig"))
	require.Len(t, s.Prepare, 2)
	assert.Equal(t, "./charts/shop", s.Prepare[0].Helm.Chart)
	assert.Equal(t, "Deployment", s.Prepare[1].Restart[0].Kind)
	assert.Equal(t, After("chart"), s.Expect[1].After)
	assert.Equal(t, filepath.Dir(s.Dir+"/x"), s.Dir)
}

func TestFromFile_UnknownKey(t *testing.T) {
	_, err := FromFile(write(t, "name: x\napiVersion: v1\n"))
	assert.Error(t, err, "no apiVersion: the parser is strict")
}

func TestFromFile_UnknownImportKey(t *testing.T) {
	_, err := FromFile(write(t, "name: x\nimports:\n  - path: ./a.yaml\n    freshCluter: true\n"))
	assert.ErrorContains(t, err, "freshCluter", "an import's keys are checked like the rest")
}

func TestValidate(t *testing.T) {
	for name, tc := range map[string]struct{ doc, want string }{
		"no action":           {"name: x\nprepare: [{name: a}]\n", "set one of apply, helm, delete or commands"},
		"two actions":         {"name: x\nprepare: [{apply: a.yaml, delete: b.yaml}]\n", "one action per step"},
		"duplicate step":      {"name: x\nprepare: [{name: a, apply: a.yaml}, {name: a, apply: b.yaml}]\n", "used by an earlier step"},
		"reserved name":       {"name: x\nprepare: [{name: prepare, apply: a.yaml}]\n", "reserved for after:"},
		"unknown after":       {"name: x\nexpect: [{name: e, after: nope}]\n", `after "nope"`},
		"duplicate expect":    {"name: x\nexpect: [{name: e}, {name: e}]\n", "used by an earlier expectation"},
		"unnamed expect":      {"name: x\nexpect: [{timeout: 5s}]\n", "name is required"},
		"bad timeout":         {"name: x\nexpect: [{name: e, timeout: soon}]\n", "is not a duration"},
		"restart kind":        {"name: x\nprepare: [{apply: a.yaml, restart: [{kind: Pod, name: p}]}]\n", "Deployment, StatefulSet or DaemonSet"},
		"bad provider":        {"name: x\nenvironment: {cluster: {provider: minikube}}\n", "environment.cluster.provider"},
		"import without path": {"name: x\nimports: [{freshCluster: true}]\n", "path is required"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := FromFile(write(t, tc.doc))
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tc.want), "%v\nwant %q", err, tc.want)
		})
	}
}

// keep leaves what apply or helm did for later imports: a step with neither
// has nothing to keep.
func TestValidate_KeepNeedsApplyOrHelm(t *testing.T) {
	_, err := FromFile(write(t, "name: x\nprepare:\n  - name: c\n    commands: [{run: \"true\"}]\n    keep: true\n"))
	assert.ErrorContains(t, err, "keep leaves what apply or helm did")
	_, err = FromFile(write(t, "name: x\nprepare:\n  - name: a\n    apply: ./a.yaml\n    keep: true\n"))
	assert.NoError(t, err)
}

// An include file can include others, each path resolved from the file that
// names it, and each expectation run from its own file's directory.
func TestExpandIncludes_Nested(t *testing.T) {
	dir := t.TempDir()
	structuredtest.WriteFile(t, filepath.Join(dir, "include", "admission.yaml"),
		"expect:\n  - include: ./admission/deny.yaml\n  - name: own check\n    commands: [{run: \"true\"}]\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "include", "admission", "deny.yaml"),
		"expect:\n  - name: deny when\n    commands: [{run: \"true\"}]\n")
	got, err := Expectations{{Include: "./include/admission.yaml"}, {Name: "top"}}.ExpandIncludes(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"deny when", "own check", "top"}, got.Names())
	assert.Equal(t, filepath.Join(dir, "include", "admission"), got[0].WorkDir)
	assert.Equal(t, filepath.Join(dir, "include"), got[1].WorkDir)
	assert.Empty(t, got[2].WorkDir)
}

// A file that includes itself, through others or directly, is an error
// naming the chain.
func TestExpandIncludes_Loop(t *testing.T) {
	dir := t.TempDir()
	structuredtest.WriteFile(t, filepath.Join(dir, "a.yaml"), "expect:\n  - include: ./b.yaml\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "b.yaml"), "expect:\n  - include: ./a.yaml\n")
	_, err := Expectations{{Include: "./a.yaml"}}.ExpandIncludes(dir)
	assert.ErrorContains(t, err, "includes itself")
	assert.ErrorContains(t, err, "a.yaml → "+filepath.Join(dir, "b.yaml")+" → "+filepath.Join(dir, "a.yaml"))
}

// An import runs on a fresh cluster or in a context, not both.
func TestValidate_ImportFreshClusterOrContext(t *testing.T) {
	_, err := FromFile(write(t, "name: x\nimports:\n  - { path: ./a.yaml, freshCluster: true, context: kind-dev }\n"))
	assert.ErrorContains(t, err, "set freshCluster or context, not both")
}

// An expectation's env is the jig's, with the expectation's own winning.
func TestExpectation_ResolveEnv(t *testing.T) {
	exp := Expectation{Env: map[string]string{"B": "exp", "C": "exp"}}
	got := exp.ResolveEnv(map[string]string{"A": "jig", "B": "jig"}).Vars()
	assert.Equal(t, env.Vars{"A": "jig", "B": "exp", "C": "exp"}, got)

	assert.Empty(t, Expectation{}.ResolveEnv(nil).Vars())
}
