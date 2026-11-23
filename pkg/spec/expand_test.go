package spec

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/tools/structured/structuredtest"
)

// Images, apply entries and Helm releases expand their includes wherever
// they are listed, nested ones too, each path resolved from the file that
// holds it; the phases keep their own entries around them.
func TestRead_IncludesOnTheTypes(t *testing.T) {
	dir := t.TempDir()
	structuredtest.WriteFile(t, filepath.Join(dir, "jig.yaml"), `name: shop
environment:
  images:
    - include: ./e2e/images.yaml
    - shop-worker:dev
  apply:
    - include: ./e2e/furniture.yaml
  helm:
    - include: ./e2e/helm/platform.yaml
prepare:
  - name: chart
    helm: { include: ./e2e/helm/shop.yaml }
  - name: order
    apply:
      - ./order.yaml
      - include: ./e2e/furniture.yaml
`)
	structuredtest.WriteFile(t, filepath.Join(dir, "e2e", "images.yaml"),
		"images:\n  - image: shop-api:dev\n    build: { context: ../api }\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "e2e", "furniture.yaml"),
		"apply:\n  - ./namespaces.yaml\n  - path: /abs/storage.yaml\n    wait: [{ kind: StorageClass, name: fast }]\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "e2e", "helm", "platform.yaml"),
		"helm:\n  - include: ./cert-manager.yaml\n  - { repo: https://charts.example, chart: argo-cd, valueFiles: [./argo.yaml, https://x/v.yaml] }\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "e2e", "helm", "cert-manager.yaml"),
		"helm:\n  - { repo: https://charts.jetstack.io, chart: cert-manager }\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "e2e", "helm", "shop.yaml"),
		"helm:\n  - { chart: ../../charts/shop, values: { image.tag: dev } }\n")

	s, err := Read(filepath.Join(dir, "jig.yaml"))
	require.NoError(t, err)
	e2e, helm := filepath.Join(dir, "e2e"), filepath.Join(dir, "e2e", "helm")

	env := s.Environment
	require.Len(t, env.Images, 2)
	assert.Equal(t, "shop-api:dev", env.Images[0].Image)
	assert.Equal(t, filepath.Join(dir, "api"), env.Images[0].Build.Context)
	assert.Equal(t, "shop-worker:dev", env.Images[1].Image)

	furniture := ApplyEntries{
		{Path: filepath.Join(e2e, "namespaces.yaml")},
		{Path: "/abs/storage.yaml", Wait: Waits{{Kind: "StorageClass", Name: "fast"}}},
	}
	assert.Equal(t, furniture, env.Apply)

	require.Len(t, env.Helm, 2)
	assert.Equal(t, "cert-manager", env.Helm[0].Chart)
	assert.Equal(t, "argo-cd", env.Helm[1].Chart)
	assert.Equal(t, []string{filepath.Join(helm, "argo.yaml"), "https://x/v.yaml"}, env.Helm[1].ValueFiles)

	require.NotNil(t, s.Prepare[0].Helm)
	assert.Equal(t, filepath.Join(dir, "charts", "shop"), s.Prepare[0].Helm.Chart)
	assert.Equal(t, append(ApplyEntries{{Path: "./order.yaml"}}, furniture...), s.Prepare[1].Apply)
}

// A step installs one release: its helm include must hold exactly one.
func TestRead_StepHelmIncludeHoldsOne(t *testing.T) {
	dir := t.TempDir()
	structuredtest.WriteFile(t, filepath.Join(dir, "jig.yaml"),
		"name: x\nprepare:\n  - name: platform\n    helm: { include: ./helm.yaml }\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "helm.yaml"),
		"helm:\n  - { repo: https://a, chart: a }\n  - { repo: https://b, chart: b }\n")
	_, err := Read(filepath.Join(dir, "jig.yaml"))
	assert.ErrorContains(t, err, `prepare "platform": helm: include ./helm.yaml: a step installs one release; the file has 2`)
}

// An entry with include sets nothing else.
func TestRead_IncludeStandsAlone(t *testing.T) {
	dir := t.TempDir()
	structuredtest.WriteFile(t, filepath.Join(dir, "jig.yaml"),
		"name: x\nenvironment:\n  helm:\n    - { include: ./helm.yaml, namespace: shop }\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "helm.yaml"), "helm:\n  - { repo: https://a, chart: a }\n")
	_, err := Read(filepath.Join(dir, "jig.yaml"))
	assert.ErrorContains(t, err, "environment.helm: include ./helm.yaml: an entry with include sets nothing else")
}

// A file that includes itself, through others or directly, is an error
// naming the chain.
func TestRead_TypeIncludeLoop(t *testing.T) {
	dir := t.TempDir()
	structuredtest.WriteFile(t, filepath.Join(dir, "jig.yaml"),
		"name: x\nenvironment:\n  images:\n    - include: ./a.yaml\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "a.yaml"), "images:\n  - include: ./b.yaml\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "b.yaml"), "images:\n  - include: ./a.yaml\n")
	_, err := Read(filepath.Join(dir, "jig.yaml"))
	assert.ErrorContains(t, err, "includes itself")
	assert.ErrorContains(t, err, filepath.Join(dir, "a.yaml")+" → "+filepath.Join(dir, "b.yaml")+" → "+filepath.Join(dir, "a.yaml"))
}

// An included image from git: a local repository's path resolves from the
// include file, its build paths stay relative to the checkout, and a remote
// URL is left as written.
func TestRead_IncludedGitImage(t *testing.T) {
	dir := t.TempDir()
	structuredtest.WriteFile(t, filepath.Join(dir, "jig.yaml"), "name: x\nenvironment:\n  images:\n    - include: ./e2e/images.yaml\n")
	structuredtest.WriteFile(t, filepath.Join(dir, "e2e", "images.yaml"), `images:
  - name: auth
    git: { url: ../../auth }
    build: { context: ./service }
  - name: api
    git: { url: https://github.com/acme/api.git, branch: main }
    build: { context: . }
`)
	s, err := Read(filepath.Join(dir, "jig.yaml"))
	require.NoError(t, err)
	imgs := s.Environment.Images
	require.Len(t, imgs, 2)
	assert.Equal(t, filepath.Join(filepath.Dir(dir), "auth"), imgs[0].Git.URL)
	assert.Equal(t, "./service", imgs[0].Build.Context, "relative to the checkout")
	assert.Equal(t, "https://github.com/acme/api.git", imgs[1].Git.URL)
	assert.Equal(t, ".", imgs[1].Build.Context)
}

// Images with forEach are expanded when the test is read, after the
// includes: a row from an include file resolves its paths from that file,
// and the run sees only concrete images.
func TestRead_ImageForEach(t *testing.T) {
	dir := t.TempDir()
	structuredtest.WriteFile(t, filepath.Join(dir, "jig.yaml"), `name: x
environment:
  images:
    - include: ./e2e/images.yaml
`)
	structuredtest.WriteFile(t, filepath.Join(dir, "e2e", "images.yaml"), `images:
  - name: "api-{{ .item }}"
    forEach: { items: [main, next] }
    image: "api:{{ .item }}"
    build: { context: ./api }
    deploy: { ports: [8080] }
`)
	s, err := Read(filepath.Join(dir, "jig.yaml"))
	require.NoError(t, err)
	imgs := s.Environment.Images
	require.Len(t, imgs, 2)
	assert.Equal(t, "api-next", imgs[1].Name)
	assert.Equal(t, "api:next", imgs[1].Image)
	assert.Equal(t, filepath.Join(dir, "e2e", "api"), imgs[1].Build.Context, "from the include file")
	assert.Nil(t, imgs[1].ForEach)
}
