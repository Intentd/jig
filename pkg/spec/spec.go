package spec

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/intentd/tools/assert"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/resolver/env"
	"github.com/intentd/tools/schemagen"
	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Provision is the set of operations that make resources exist in the
// cluster: manifests to apply, charts to install, resources to wait for.
//
// It is used by the environment phase (cluster furniture: a namespace, an
// ingress controller, a storage class) and by the prepare phase (the jig's
// dependencies and the workload under test). Simulate uses it too, for the
// resources a simulated run needs before it starts.
//
// Both phases embed it inline, so the YAML is flat — environment.apply and
// prepare.apply, not environment.provision.apply.
type Provision struct {
	// Apply is an ordered list of manifests to kubectl-apply.
	// Each entry is either a plain path string or a {path, wait} struct.
	Apply ApplyEntries `yaml:"apply,omitempty"`

	// Helm is an ordered list of Helm charts to install.
	// Executed as helm upgrade --install.
	Helm HelmInstalls `yaml:"helm,omitempty"`

	// Wait blocks until all listed resources exist and satisfy conditions.
	// Runs after all apply and helm steps. Use per-entry wait for ordered checks.
	Wait Waits `yaml:"wait,omitempty"`
}

// ApplyShorthand returns the apply entries for the plain-list shorthand
// (a sequence of path strings), or nil when value is not that shorthand.
func ApplyShorthand(value *yaml.Node) ApplyEntries {
	if value.Kind != yaml.SequenceNode {
		return nil
	}
	var entries ApplyEntries
	for _, item := range value.Content {
		if item.Kind != yaml.ScalarNode {
			break
		}
		entries = append(entries, ApplyEntry{Path: item.Value})
	}
	return entries
}

// ApplyEntry is a single manifest to kubectl-apply during setup.
// It is either a plain path string or a struct with an optional per-entry wait.
//
//	# flat form
//	- ./prereqs/secret.yaml
//
//	# structured form
//	- path: ./prereqs/secret.yaml
//	  wait:
//	    - kind: Secret
//	      name: my-secret
//	      namespace: default
//	      timeout: 30s
type ApplyEntry struct {
	// Path is the YAML file path to kubectl-apply.
	Path string `yaml:"path"`
	// Wait blocks after this apply until all listed resources satisfy their conditions.
	Wait Waits `yaml:"wait,omitempty"`
	// Generated marks a file the caller writes at run time, such as a
	// manifest it generates: an offline check doesn't look for it. Set in
	// code, never read from the file.
	Generated bool `yaml:"-"`
	// Include names a file whose apply entries replace this one, their
	// paths resolved from that file. It stands alone.
	Include string `yaml:"include,omitempty"`
}

// UnmarshalYAML allows a ApplyEntry to be written as either a plain string
// (the path) or a full {path, wait} struct.
func (e *ApplyEntry) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		e.Path = value.Value
		return nil
	}
	type plain ApplyEntry
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return strictUnmarshal(raw, (*plain)(e))
}

// JSONSchemaExtend accepts a path beside the entry, as UnmarshalYAML does.
func (ApplyEntry) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s, schemagen.Str("Path to a manifest"))
}

// HelmInstall installs a Helm chart as a real release into the cluster.
// Unlike HelmSource (which renders charts to extract Blueprint documents),
// this runs helm upgrade --install for a prerequisite chart.
type HelmInstall struct {
	// Repo is the Helm repository URL. Omit for local chart paths.
	Repo string `yaml:"repo,omitempty"`
	// Chart is the chart name within the repository, or a local path (e.g. ./).
	Chart string `yaml:"chart"`
	// Release is the Helm release name. Defaults to the chart name when empty.
	Release string `yaml:"release,omitempty"`
	// Namespace for the release. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Version pins the chart version. Leave empty for latest or local charts.
	Version string `yaml:"version,omitempty"`
	// ValueFiles is an ordered list of values files (local paths or URLs).
	ValueFiles []string `yaml:"valueFiles,omitempty"`
	// Values are inline key-value overrides, equivalent to helm --set.
	Values map[string]interface{} `yaml:"values,omitempty"`
	// CreateNamespace passes --create-namespace to helm.
	CreateNamespace bool `yaml:"createNamespace,omitempty"`
	// Wait blocks after this helm install until all listed resources satisfy conditions.
	Wait Waits `yaml:"wait,omitempty"`
	// Include names a file whose releases replace this one, their local
	// chart and values paths resolved from that file. It stands alone.
	Include string `yaml:"include,omitempty"`
}

// HelmRelease returns the release to install.
func (h HelmInstall) HelmRelease() cluster.HelmRelease {
	return cluster.HelmRelease{
		Repo: h.Repo, Chart: h.Chart, Release: h.Release, Namespace: h.Namespace,
		Version: h.Version, ValueFiles: h.ValueFiles, Values: h.Values,
		CreateNamespace: h.CreateNamespace,
	}
}

// ReleaseName returns the effective Helm release name.
func (h HelmInstall) ReleaseName() string { return h.HelmRelease().Name() }

// EffectiveNamespace returns the effective namespace, defaulting to "default".
func (h HelmInstall) EffectiveNamespace() string {
	return h.HelmRelease().EffectiveNamespace()
}

// IsLocalChart reports whether the chart field is a local filesystem path.
func (h HelmInstall) IsLocalChart() bool { return h.HelmRelease().IsLocalChart() }

// Validate returns an error when required fields are missing.
func (h HelmInstall) Validate() error {
	if err := h.HelmRelease().Validate(); err != nil {
		return fmt.Errorf("setup.helm: %w", err)
	}
	return nil
}

// Wait describes a single resource to wait for before the operator starts.
type Wait struct {
	// Kind is the Kubernetes resource kind (e.g. Deployment, Secret, Namespace).
	Kind string `yaml:"kind"`
	// Name is the exact resource name.
	Name string `yaml:"name"`
	// Namespace to look in. Omit for cluster-scoped resources.
	Namespace string `yaml:"namespace,omitempty"`
	// Ready waits for an available/ready condition, not just existence.
	Ready bool `yaml:"ready,omitempty"`
	// Timeout is a Go duration string. Default: "30s".
	Timeout string `yaml:"timeout,omitempty"`
}

// Environment is the first phase of a run: the cluster it executes against
// and the infrastructure it needs before the jig's own dependencies are
// installed.
//
// The cluster is either created (kind) or selected (an existing context).
// Images are built and loaded into it. Apply, Helm and Wait provision cluster
// furniture — a namespace, an ingress controller, a storage class — that the
// prepare phase's charts and manifests expect to exist.
type Environment struct {
	// Cluster controls which cluster to use and how it is provisioned.
	Cluster Cluster `yaml:"cluster,omitempty"`

	// Images lists container images to build and load into the cluster before
	// the test runs. Kind-only: each image is loaded with `kind load`.
	//
	// Manifests that reference these images must set imagePullPolicy to
	// IfNotPresent or Never. A manifest with imagePullPolicy: Always ignores
	// the loaded image and pulls from the registry instead.
	//
	// The CLI flag --images narrows this list; it cannot add images the
	// schema does not declare. When this list is empty, the CLI flag is the
	// only source — the CI case, where the just-built image is the point.
	Images cluster.Images `yaml:"images,omitempty"`

	// Ecosystem is the data services the workload uses (Postgres, MariaDB,
	// RabbitMQ, Redis, Kafka) and the Secrets with their connection details,
	// installed after the images and before apply, helm and wait.
	Ecosystem *Ecosystem `yaml:"ecosystem,omitempty"`

	Provision `yaml:",inline"`
}

// Mode is how a run gets its cluster: created, existing, or none at all.
type Mode string

const (
	// ModeCluster creates or uses a cluster: kind by default, or an
	// existing context.
	ModeCluster Mode = "cluster"
	// ModeCommands runs without a cluster: only commands steps and
	// expectations that need no Kubernetes API.
	ModeCommands Mode = "commands"
)

// String returns the mode name, for messages and flags.
func (m Mode) String() string { return string(m) }

// Mode reports how the run gets its cluster: commands when the provider is
// none, cluster otherwise. nil-safe: no environment means no cluster
// declared, which is the kind default.
func (e *Environment) Mode() Mode {
	if e.Provider() == ProviderNone {
		return ModeCommands
	}
	return ModeCluster
}

// UnmarshalYAML allows environment: to be written as a plain list of
// manifest paths, equivalent to environment.apply:
//
//	environment:
//	  - ./prereqs/namespace.yaml
func (e *Environment) UnmarshalYAML(value *yaml.Node) error {
	if entries := ApplyShorthand(value); len(entries) > 0 {
		e.Apply = append(e.Apply, entries...)
		return nil
	}
	type plain Environment
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return strictUnmarshal(raw, (*plain)(e))
}

// HasImage reports whether the environment declares any images.
func (e *Environment) HasImage() bool {
	return e != nil && len(e.Images) > 0
}

// Provider returns the cluster provider this run uses, defaulting to kind
// when no environment is declared. nil-safe: a spec without an environment
// still runs against a kind cluster.
func (e *Environment) Provider() ClusterProvider {
	if e == nil {
		return ProviderKind
	}
	return e.Cluster.EffectiveProvider()
}

// ClusterName returns the kind cluster's name, or fallback when no
// environment is declared or the name is empty. nil-safe.
func (e *Environment) ClusterName(fallback string) string {
	if e == nil || e.Cluster.Name == "" {
		return fallback
	}
	return e.Cluster.Name
}

// Reuse reports whether an existing cluster is reused instead of recreated.
// nil-safe: no environment means no reuse decision, so false.
func (e *Environment) Reuse() bool {
	if e == nil {
		return false
	}
	return e.Cluster.Reuse
}

// EffectiveLoadImages resolves the image references this run should load.
//
// If the environment is nil or declares no images, the CLI image list is
// returned, deduplicated.
//
// If no CLI images are supplied, all declared images that need loading are
// returned in declaration order.
//
// If CLI images are supplied alongside declared images, they must be a
// subset of the declared references. Only selected images that need loading
// are returned.
//
// An image with deploy configured but no build is treated as a registry
// image: it is deployed directly and does not need loading into kind.
func (e *Environment) EffectiveLoadImages(
	specName string,
	cliImages []string,
) ([]string, error) {
	cli := dedupeStrings(cliImages)

	if e == nil || len(e.Images) == 0 {
		return cli, nil
	}

	selected, err := e.selectedImages(specName, cli)
	if err != nil {
		return nil, err
	}

	// Each reference once: one entry can build an image that others only
	// use (deploy it again, say), and it is built and loaded once.
	refs := make([]string, 0, len(selected))
	for _, img := range selected {
		if img.Deploy != nil && img.Build == nil {
			continue
		}
		if !slices.Contains(refs, img.Image) {
			refs = append(refs, img.Image)
		}
	}

	return refs, nil
}

// EffectiveDeployImages resolves the declared images this run should deploy.
//
// If the environment is nil or declares no images, no images can be deployed.
//
// If no CLI images are supplied, all declared images with deploy configuration
// are returned in declaration order.
//
// If CLI images are supplied, they must be a subset of the declared image
// references. Only selected images with deploy configuration are returned.
//
// The returned images have their default image references, deployment names,
// and other resolved defaults applied.
func (e *Environment) EffectiveDeployImages(
	specName string,
	cliImages []string,
) (cluster.Images, error) {
	if e == nil || len(e.Images) == 0 {
		return nil, nil
	}

	selected, err := e.selectedImages(specName, dedupeStrings(cliImages))
	if err != nil {
		return nil, err
	}

	deployments := make(cluster.Images, 0, len(selected))
	for _, img := range selected {
		if img.Deploy == nil {
			continue
		}
		deployments = append(deployments, img)
	}

	return deployments, nil
}

// selectedImages resolves the declared image defaults and applies the CLI
// selection. It returns all declared images when the CLI selection is empty.
//
// When the CLI selection is non-empty, every requested reference must match
// a declared image. The returned images retain their original declaration
// order, regardless of the order of the CLI arguments.
func (e *Environment) selectedImages(
	specName string,
	cliImages []string,
) (cluster.Images, error) {
	declared := e.Images.ResolveDefaultNames(specName)

	if len(cliImages) == 0 {
		return declared, nil
	}

	allowed := make(map[string]struct{}, len(declared))
	for _, img := range declared {
		allowed[img.Image] = struct{}{}
	}

	for _, ref := range cliImages {
		if _, ok := allowed[ref]; !ok {
			refs := make([]string, 0, len(declared))
			for _, img := range declared {
				refs = append(refs, img.Image)
			}

			return nil, fmt.Errorf(
				"environment.images: --images %q is not declared in the schema (declared: %s)",
				ref,
				strings.Join(refs, ", "),
			)
		}
	}

	selected := make(cluster.Images, 0, len(cliImages))
	set := make(map[string]struct{}, len(cliImages))
	for _, ref := range cliImages {
		set[ref] = struct{}{}
	}

	for _, img := range declared {
		if _, ok := set[img.Image]; ok {
			selected = append(selected, img)
		}
	}

	return selected, nil
}

// dedupeStrings trims image references, drops empty values, and preserves
// the order of their first occurrence.
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// Import references another E2E file to run after this one completes.
// By default imports share the same cluster. Set freshCluster: true to
// provision a new cluster for that import instead.
//
// Shorthand — a plain path string is equivalent to {path: <string>}:
//
//	imports:
//	  - ./auth-e2e.yaml
//	  - ./rbac-e2e.yaml
//	  - path: ./infra-e2e.yaml
//	    freshCluster: true
type Import struct {
	// Path is the path to another E2E spec file (must be kind: E2E).
	Path string `yaml:"path"`
	// FreshCluster provisions a new kind cluster for this import instead of
	// reusing the parent's cluster. Default: false (share parent cluster).
	FreshCluster bool `yaml:"freshCluster,omitempty"`
	// Context runs the import in this kubeconfig context, a cluster jig
	// neither creates nor deletes, instead of the parent's. Set it or
	// FreshCluster, not both.
	Context string `yaml:"context,omitempty"`
	// Wait is an optional duration to sleep before this import starts.
	// Useful when the previous test leaves cluster state that needs time to
	// clear — webhook deregistration, namespace termination, cert provisioning.
	// Must be a valid Go duration string (e.g. "10s", "1m30s").
	Wait string `yaml:"wait,omitempty"`
}

// Read reads and returns the jig the import references.
// Relative paths are resolved against the importing declaration's directory.
func (i Import) Read(baseDir string) (*Declaration, error) {
	path := i.Path

	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}

	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving import %q: %w", i.Path, err)
	}

	return Read(path)
}

// UnmarshalYAML allows imports to be written as a plain string path.
func (i *Import) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		i.Path = value.Value
		return nil
	}
	type plain Import
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return strictUnmarshal(raw, (*plain)(i))
}

// JSONSchemaExtend accepts a path beside the entry, as UnmarshalYAML does.
func (Import) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s, schemagen.Str("Path to another jig"))
}

// OnFailure declares operations to run when an expectation fails.
type OnFailure struct {
	SideEffects `yaml:",inline"`

	// Stop halts the run after the diagnostics for this failure. At spec
	// level it applies to any expectation that fails; at expectation level it
	// applies only to that one. The failure is still recorded and reported;
	// Stop only decides whether later expectations run.
	Stop *bool `yaml:"stop,omitempty"`
}

// Finally declares operations to run after an expectation completes —
// whether it passed, failed or was skipped — before the next expectation
// runs. Use it to undo what a step changed in files or cluster state.
type Finally struct {
	SideEffects `yaml:",inline"`
}

// SideEffects is a post block: the operations onFailure: (on failure) and
// finally: (always) run. They are a step's operations — commands, kubectl
// with every verb, external — in the same shape and run by the same code,
// with the expectation's env. What differs is the policy: a post block runs
// each operation once, in order, and never stops early; it prints each one's
// output; and it never changes the result. A failed operation or a missed
// assertion is reported as a warning on the case, so a broken cleanup is
// visible but cannot fail a passing case or hide why a case failed.
type SideEffects struct {
	// Commands — shell commands, the same as a step's.
	Commands Commands `yaml:"commands,omitempty"`
	// Kubectl — the same block as a step's: writes (apply, patch, restart,
	// scale, delete), then reads (get, logs, describe, …).
	Kubectl *Kubectl `yaml:"kubectl,omitempty"`
	// External — calls, the same as a step's.
	External ExternalChecks `yaml:"external,omitempty"`
}

// Empty reports whether no operation is declared.
func (s *SideEffects) Empty() bool {
	return s == nil || (s.Kubectl == nil && len(s.Commands) == 0 && len(s.External) == 0)
}

// HasOnFailure reports whether this expectation declares failure diagnostics.
func (e *Expectation) HasOnFailure() bool {
	return e.OnFailure != nil && !e.OnFailure.Empty()
}

// HasFinally reports whether this expectation declares post-step operations.
func (e *Expectation) HasFinally() bool {
	return e.Finally != nil && !e.Finally.Empty()
}

// EffectiveStop reports whether this expectation failing should stop the run
// after diagnostics. An expectation-level onFailure.stop wins; otherwise the
// spec-level value applies.
func (e *Expectation) EffectiveStop(specOnFailure *OnFailure) bool {
	if e.OnFailure != nil && e.OnFailure.Stop != nil {
		return *e.OnFailure.Stop
	}
	return specOnFailure != nil && specOnFailure.Stop != nil && *specOnFailure.Stop
}

// ClusterProvider is the cluster backend an E2E run uses.
type ClusterProvider string

const (
	// ProviderKind creates an ephemeral kind cluster and deletes it after.
	ProviderKind ClusterProvider = "kind"
	// ProviderNone runs without a cluster: only commands: may be used.
	// cluster.provider: none implies no bundle, no Inrun install, no context switch.
	ProviderNone ClusterProvider = "none"
)

// String returns the annotation/CLI form (stable).
func (p ClusterProvider) String() string { return string(p) }

// validClusterProviders is the authoritative set used by inrun validate.
var validClusterProviders = []string{
	ProviderKind.String(), ProviderNone.String(),
}

// ValidClusterProviders returns the supported providers, in display order.
func ValidClusterProviders() []string {
	return validClusterProviders
}

// IsValid reports whether p is a known provider.
func (p ClusterProvider) IsValid() bool {
	for _, prov := range validClusterProviders {
		if p.String() == prov {
			return true
		}
	}
	return false
}

// RequiresCluster reports whether this provider needs a reachable cluster.
func (p ClusterProvider) RequiresCluster() bool {
	return p != ProviderNone
}

// EffectiveProvider returns the declared provider, defaulting to kind.
func (c Cluster) EffectiveProvider() ClusterProvider {
	if c.Provider == "" {
		return ProviderKind
	}
	return c.Provider
}

// Cluster controls cluster creation and selection.
type Cluster struct {
	// Provider is the cluster backend. Defaults to kind.
	Provider ClusterProvider `yaml:"provider,omitempty"`
	// Name is the kind cluster name. Default: "inrun-e2e".
	Name string `yaml:"name"`
	// Reuse controls whether an existing cluster is reused or recreated.
	// false (default) — delete and recreate for a clean state.
	// true — reuse existing cluster if it exists.
	Reuse bool `yaml:"reuse"`
}

// After is the lifecycle event that triggers an expectation block.
type After string

// Expectation is one named assertion block.
type Expectation struct {
	// Include is a path to a YAML file containing a list of Expectation entries.
	// When set, the file is loaded and its entries are expanded in place of this entry.
	// All other fields must be empty when include is set.
	// Relative paths resolve from the e2e.yaml directory.
	Include string `yaml:"include,omitempty"`

	// Name is printed in the results table.
	Name string `yaml:"name"`
	// After is the lifecycle event that triggers this expectation.
	// Valid values: "setup-complete", "cr-applied", "cr-deleted".
	After After `yaml:"after"`
	// Timeout is the maximum time to wait for the expectation to pass.
	Timeout string `yaml:"timeout"` // e.g. "60s"
	// Stays is how long the checks must then go on passing, polled: a
	// window in which the first failure fails the expectation ("no
	// restarts for 2m"). Empty: passing once is enough.
	Stays string `yaml:"stays,omitempty"`
	// Order asserts that the objects' timestamps come in the order listed:
	// each later than the one before.
	Order []OrderEntry `yaml:"order,omitempty"`
	// Wait is an optional duration to sleep before the polling loop starts.
	// Useful when the previous step needs time to take effect before assertions
	// should begin — e.g. waiting for enough reconcile cycles to accumulate.
	// Must be a valid Go duration string (e.g. "10s", "30s").
	Wait string `yaml:"wait,omitempty"`

	// Resources is a unified list of resource checks across any kind.
	Resources []ResourceCheck `yaml:"resources,omitempty"`

	// Commands are shell commands checked in the same polling loop as resources.
	Commands Commands `yaml:"commands,omitempty"`

	// Kubectl is the structured kubectl DSL block.
	// An alternative to commands: for common kubectl operations — get, logs,
	// describe, exec, port-forward. Compiles to kubectl invocations internally.
	// Use commands: for anything that doesn't fit a subcommand.
	Kubectl *Kubectl `yaml:"kubectl,omitempty"`

	// External makes the calls a Blueprint's external: makes — HTTP,
	// Prometheus, Postgres, Redis, Mongo, Kafka — from the machine running the
	// test, and asserts on what they return. The step passes when every call
	// succeeds and its assertions hold.
	External ExternalChecks `yaml:"external,omitempty"`

	// When — all conditions must pass for this expectation to run (AND).
	// Empty means unconditional. Functions declared in spec.functions are available as
	// template expressions: field: '{{ inBusinessHours }}'.
	When condition.Conditions `yaml:"when,omitempty"`
	// Or — at least one condition must pass, alongside When if declared..
	// Functions declared in spec.functions are available as
	// template expressions: field: '{{ inBusinessHours }}'.
	Or condition.Conditions `yaml:"or,omitempty"`

	// OnFailure declares diagnostic operations to run immediately when this
	// specific expectation fails. Runs before moving to the next expectation.
	OnFailure *OnFailure `yaml:"onFailure,omitempty"`

	// Finally declares operations to run after this expectation completes —
	// whether it passed, failed or was skipped — before the next expectation
	// runs. It is a post block (see SideEffects): its failures are
	// warnings, never the case's result. Use it to undo what the step changed
	// in files or cluster state.
	Finally *Finally `yaml:"finally,omitempty"`

	// WorkDir is the directory used to resolve relative file paths in this
	// expectation. Set by ExpandIncludes to the directory of the include
	// file; empty means use the e2e.yaml directory. Read it through Dir.
	WorkDir string `yaml:"-"`

	// Env declares environment variables available to this expectation.
	// Overrides variables declared in the parent E2E spec. ${NAME} in every
	// other field of the expectation resolves through it.
	Env map[string]string `yaml:"env,omitempty" env:"-"`

	// Retry is the maximum retry count for one expectation.
	Retry int `yaml:"retry,omitempty"`
}

// Dir is the directory this expectation's relative files resolve from: its
// include file's directory for an included step, else suiteDir, the suite
// file's. Running, include expansion and validation all resolve through it,
// so a path that validates is the path that runs.
func (e Expectation) Dir(suiteDir string) string {
	if e.WorkDir != "" {
		return e.WorkDir
	}
	return suiteDir
}

// ExpectationScope is the per-expectation input bundle: what a step, its
// post blocks and their operations need for one expectation, without
// threading six parameters through every call.
type ExpectationScope struct {
	Expectation Expectation
	WorkDir     string
	CS          kubernetes.Interface
	Cfg         *rest.Config
	Dynamic     dynamic.Interface
	Mapper      apimeta.RESTMapper
	Wrote       *bool // the step's kubectl writes have succeeded this attempt
	// Capture saves values a check captured (capture:), by name, for the
	// expectations after this one.
	Capture func(map[string]string)
	Env     env.Resolver
	CRData  map[string]interface{} // primary CR's object data, for when:/or: conditions
}

// ResourceCheck asserts the state of any Kubernetes resource.
// Set kind to: Deployment, Service, Secret, ConfigMap.
type ResourceCheck struct {
	// Kind is the resource kind: Deployment, Service, Secret, ConfigMap.
	Kind string `yaml:"kind"`
	// Name is the resource name. Empty means any resource of this kind in the namespace.
	Name string `yaml:"name,omitempty"`
	// Namespace to look in. Empty means "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Count asserts the exact number of matching resources. nil means "at least 1".
	// Set to 0 to assert none exist (cleanup check).
	Count *int `yaml:"count,omitempty"`
	// Ready asserts at least one available replica (Deployments only).
	Ready bool `yaml:"ready,omitempty"`
}

// Command runs a shell command and asserts its result.
type Command struct {
	// Run is a shell command string executed via sh -c. Only ${{ env.NAME }}
	// is expanded first, as in GitHub Actions: the shell expands $NAME and
	// ${NAME} itself, from the process environment the declared env is added
	// to.
	Run string `yaml:"run" env:"shell"`
	// ExitCode is the expected exit code. Default 0 (success).
	// Set to non-zero to assert the command must fail — useful for
	// admission webhook rejection tests.
	ExitCode int `yaml:"exitCode,omitempty"`

	// assert.Extractor selects one value from a command's output before assertions run.
	assert.Extractor `yaml:",inline"`

	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the command's output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
	// JSON is a list of path/value assertions on the command's JSON output,
	// without nesting under assert:.
	JSON assert.JSONAssertions `yaml:"json,omitempty"`

	// Assert checks the output with the full block: also supports text: lists
	// and json: path-scoped comparisons.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
}

// Kubectl is the structured kubectl DSL block.
// Sits alongside resources: and commands: in each expect entry.
// Each subcommand maps directly to the kubectl command people already know.
type Kubectl struct {
	// Get asserts field values on Kubernetes resources.
	// Generates: kubectl get <kind> <name> -n <ns> -o jsonpath='<field>'
	Get []KubectlGet `yaml:"get,omitempty"`
	// Logs asserts container log output.
	// Generates: kubectl logs -n <ns> -l <selector> -c <container> --since=<since>
	Logs []KubectlLogs `yaml:"logs,omitempty"`
	// Describe asserts kubectl describe output — useful for events and conditions.
	// Generates: kubectl describe <kind> <name> -n <ns>
	Describe []KubectlDescribe `yaml:"describe,omitempty"`
	// Exec runs a command inside a running container and asserts its output.
	// Generates: kubectl exec -n <ns> <pod> -c <container> -- <command>
	Exec []KubectlExec `yaml:"exec,omitempty"`
	// PortForward opens a port-forward to a service or pod, makes an HTTP request,
	// and asserts the response. EnsureCurl is called automatically when any entry
	// declares a path. Generates: kubectl port-forward + curl.
	PortForward []KubectlPortForward `yaml:"port-forward,omitempty"`
	// Apply applies manifests from a file path or inline YAML/JSON content.
	// Generates: kubectl apply -f <file>  or  echo '<inline>' | kubectl apply -f -
	Apply []KubectlApply `yaml:"apply,omitempty"`
	// Delete deletes resources by file path or by resource identity.
	// Generates: kubectl delete -f <file>  or  kubectl delete <kind> <name> -n <ns>
	Delete []KubectlDelete `yaml:"delete,omitempty"`
	// Patch patches a Kubernetes resource with a merge, strategic, or JSON patch.
	// Generates: kubectl patch <kind> <name> -n <ns> --type=<type> -p '<patch>'
	Patch []KubectlPatch `yaml:"patch,omitempty"`
	// Events lists Kubernetes events for a specific resource and asserts the output.
	// Generates: kubectl events --for=<kind>/<name> -n <ns>
	Events []KubectlEvents `yaml:"events,omitempty"`
	// Auth checks permissions via kubectl auth can-i and asserts the result.
	// Generates: kubectl auth can-i <verb> <resource> [-n <ns>] [--as <as>]
	Auth []KubectlAuth `yaml:"auth,omitempty"`
	// Cp copies a file out of a container and asserts its content.
	// Generates: kubectl cp <ns>/<pod>:<src> <tempfile>
	Cp []KubectlCp `yaml:"cp,omitempty"`
	// Top queries live CPU and memory usage via kubectl top and asserts the output.
	// Requires metrics-server; installed automatically when entries are present.
	// Generates: kubectl top <kind> [-n <ns>] [<name> | -l <selector>] [--containers]
	Top []KubectlTop `yaml:"top,omitempty"`

	// Restart triggers a rollout restart of a Deployment, StatefulSet, or DaemonSet.
	// Generates: kubectl rollout restart <kind>/<name> -n <ns>
	// Waits for the rollout to complete when ready: true (default).
	// The expect step's timeout governs how long to wait.
	Restart []KubectlRestart `yaml:"restart,omitempty"`

	// Scale sets the replica count on a Deployment, StatefulSet, or ReplicaSet.
	// Generates: kubectl scale <kind>/<name> --replicas=<n> -n <ns>
	// Waits for the rollout to complete when ready: true (default).
	// The expect step's timeout governs how long to wait.
	Scale []KubectlScale `yaml:"scale,omitempty"`
}

// ExternalCheck is one call an e2e step makes and asserts on. It is an
// ExternalCall, so every protocol, auth.secretRef and include work as in a
// Blueprint: include: is replaced by its file's calls:, which may carry their
// own jq and assertions — a Blueprint's calls file is one without them.
// Templates see spec.functions and .inrun, with .inrun.inPod false — the call
// is made from the machine running the test.
type ExternalCheck struct {
	ExternalCall `yaml:",inline"`
	// JQ in assert.Extractor is a jq expression over the call's result — what a Blueprint reads as
	// .external.<name>: status, body and json (the decoded body) for HTTP;
	// result, rows and rowCount for Postgres; result for the other protocols.
	// Without it, the assertions apply to the body for HTTP and to result
	// for the other protocols.
	// assert.Extractor selects one value from a command's output before assertions run.
	assert.Extractor `yaml:",inline"`
	// Assert checks the extracted value. With none, the call succeeding is
	// the check.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
	// Repeat makes the call this many times and asserts on the summary
	// instead of a single result: {count, errors, p50, p95, p99, max, mean},
	// durations in milliseconds. A budget, not a load test.
	Repeat int `yaml:"repeat,omitempty"`
}

type ExternalChecks []ExternalCheck

// OrderEntry is one object in an order: check, and the path to its
// timestamp (RFC 3339).
type OrderEntry struct {
	Kind      string `yaml:"kind"`
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace,omitempty"`
	// At is the timestamp's path: .metadata.creationTimestamp,
	// .status.completionTime, …
	At string `yaml:"at"`
}

// KubectlGet asserts a field value on a Kubernetes resource.
type KubectlGet struct {
	// Kind is the Kubernetes resource kind (e.g. Deployment, ConfigMap).
	Kind string `yaml:"kind"`
	// Name is the resource name.
	Name string `yaml:"name"`
	// LabelSelector selects pods by label (e.g. "app=my-service").
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Field is a jsonpath expression to extract before asserting.
	// e.g. .spec.template.spec.containers[0].resources.requests.cpu
	Field string `yaml:"field,omitempty"`
	// Format outputs the full resource as yaml or json for looser substring assertions.
	// Ignored when field is set. Use with jq (json) or yq (yaml) for structured extraction.
	Format string `yaml:"format,omitempty"` // yaml | json
	// assert.Extractor selects one value from a command's output before assertions run.
	assert.Extractor `yaml:",inline"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
	// JSON is a list of path/value assertions on the extracted JSON output,
	// without nesting under assert:.
	JSON assert.JSONAssertions `yaml:"json,omitempty"`
	// Match is what the object must contain, in its own shape: every key
	// given must be there and match, lists element by element, a literal
	// equal and a ${ … } or {{ … }} value an expression that must be true
	// (value, object). It is flattened into json: assertions (MatchAssertions).
	Match map[string]any `yaml:"match,omitempty"`
	// Objects is how many of the objects a labelSelector selects must
	// match: all (the default) or any.
	Objects string `yaml:"objects,omitempty"`
	// Settles is how long the object's resourceVersion must stay the same:
	// it has stopped changing (no reconcile loop, no controllers fighting).
	Settles string `yaml:"settles,omitempty"`
	// Snapshot is a file the object, normalized (identity, timestamps,
	// managed fields and status left out), must equal.
	Snapshot *Snapshot `yaml:"snapshot,omitempty"`
}

// Values of KubectlGet.Objects.
const (
	ObjectsAll = "all"
	ObjectsAny = "any"
)

// MatchAssertions flattens match into json: assertions, one per leaf (tools'
// assert.MatchAssertions: the rules are tools', for every assertion).
func MatchAssertions(match map[string]any) assert.JSONAssertions {
	return assert.MatchAssertions(match)
}

// KubectlLogs asserts container log output.
type KubectlLogs struct {
	// Name is the pod name. Use LabelSelector or LeaderElection instead.
	Name string `yaml:"name,omitempty"`
	// LabelSelector selects pods by label (e.g. "app=my-service").
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// LeaderElection resolves the target pod from a Kubernetes Lease holder.
	// Mutually exclusive with Name and LabelSelector.
	LeaderElection *KubectlLeaderElection `yaml:"leaderElection,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Container name. Defaults to the first container.
	Container string `yaml:"container,omitempty"`
	// Since limits log output to the given duration (e.g. "30s", "2m").
	Since string `yaml:"since,omitempty"`
	// Pods, with LabelSelector, checks each selected pod's full log on its own:
	// "any" passes when at least one pod's log passes, "all" when every one
	// does. Use it when replicas log different things, e.g. only the one that
	// did a piece of work logs its result. Empty checks the pods' logs merged.
	// Valid values: "any", "all".
	Pods LogsPods `yaml:"pods,omitempty"`
	// assert.Extractor selects one value from a command's output before assertions run.
	assert.Extractor `yaml:",inline"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
}

// LogsPods is how kubectl.logs checks the pods its labelSelector matches.
type LogsPods string

const (
	// LogsPodsAny passes when at least one pod's log passes.
	LogsPodsAny LogsPods = "any"
	// LogsPodsAll passes when every pod's log passes.
	LogsPodsAll LogsPods = "all"
)

// ValidLogsPodsValues is the set of valid values for KubectlLogs.Pods.
var ValidLogsPodsValues = []LogsPods{LogsPodsAny, LogsPodsAll}

// KubectlDescribe asserts kubectl describe output.
// Useful for checking events, conditions, and resource details.
type KubectlDescribe struct {
	// Kind is the Kubernetes resource kind.
	Kind string `yaml:"kind"`
	// Name is the resource name. Use LabelSelector to match by label instead.
	Name string `yaml:"name,omitempty"`
	// LabelSelector selects resources by label.
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
}

// KubectlExec runs a command inside a running container and asserts its output.
type KubectlExec struct {
	// Name is the pod name. Use LabelSelector or LeaderElection to match instead.
	Name string `yaml:"name,omitempty"`
	// LabelSelector selects the pod by label.
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// LeaderElection resolves the target pod from a Kubernetes Lease holder.
	// Mutually exclusive with name and labelSelector.
	LeaderElection *KubectlLeaderElection `yaml:"leaderElection,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Container name. Defaults to the first container.
	Container string `yaml:"container,omitempty"`
	// Command is the command to execute inside the container.
	Command []string `yaml:"command"`
	// assert.Extractor selects one value from a command's output before assertions run.
	assert.Extractor `yaml:",inline"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
}

// KubectlApply applies one or more manifests during an expect checkpoint.
// Use file to reference a path on disk or inline to embed the manifest directly.
// A plain string is shorthand for file. kubectl apply is idempotent so
// re-running inside the poll loop is safe.
type KubectlApply struct {
	// File is a path to a manifest file. Relative paths resolve from the
	// directory of the file that declares the step: an include file's own.
	// Generates: kubectl apply -f <file>
	File string `yaml:"file,omitempty"`
	// Inline is a raw YAML or JSON manifest string applied via stdin.
	// Generates: echo '<inline>' | kubectl apply -f -
	Inline string `yaml:"inline,omitempty"`
	// Namespace overrides the namespace for resources that don't declare one.
	Namespace string `yaml:"namespace,omitempty"`

	// ExitCode is the expected exit code. Default 0 (success). Set to
	// non-zero to assert the apply must be rejected — e.g. an admission
	// webhook denial — instead of treating any failure as a broken test.
	// Same convention as Command.ExitCode.
	ExitCode int `yaml:"exitCode,omitempty"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
}

// KubectlDelete deletes resources during an expect checkpoint.
// Use file to delete all resources in a manifest, kind+name for a single resource,
// or leaderElection to delete the current leader pod by resolving the Lease holder.
// A plain string is shorthand for file.
type KubectlDelete struct {
	// File is a path to a manifest file. All resources in the file are deleted.
	// Relative paths resolve from the directory of the file that declares the
	// step: an include file's own.
	// Generates: kubectl delete -f <file>
	File string `yaml:"file,omitempty"`
	// Kind is the Kubernetes resource kind. Used with name for single-resource deletion.
	Kind string `yaml:"kind,omitempty"`
	// Name is the resource name.
	Name string `yaml:"name,omitempty"`
	// Namespace to target. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// LeaderElection resolves the pod to delete by reading the holder of a Kubernetes Lease.
	// Mutually exclusive with file, kind, and name.
	LeaderElection *KubectlLeaderElection `yaml:"leaderElection,omitempty"`
	// IgnoreNotFound silences errors when the resource does not exist.
	IgnoreNotFound bool `yaml:"ignoreNotFound,omitempty"`
	// --timeout=<dur>; empty means kubectl's default
	Timeout string
	// --wait=false when set to false; nil means kubectl's default (wait)
	Wait *bool
}

// UnmarshalYAML accepts a plain string as shorthand for file.
func (a *KubectlApply) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		a.File = value.Value
		return nil
	}
	type plain KubectlApply
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return strictUnmarshal(raw, (*plain)(a))
}

// JSONSchemaExtend accepts a path beside the entry, as UnmarshalYAML does.
func (KubectlApply) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s, schemagen.Str("Path to a manifest to apply (shorthand for file)"))
}

// UnmarshalYAML accepts a plain string as shorthand for file.
func (d *KubectlDelete) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		d.File = value.Value
		return nil
	}
	type plain KubectlDelete
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return strictUnmarshal(raw, (*plain)(d))
}

// JSONSchemaExtend accepts a path beside the entry, as UnmarshalYAML does.
func (KubectlDelete) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s, schemagen.Str("Path to a manifest whose resources to delete (shorthand for file)"))
}

// KubectlPatch patches Kubernetes resources in place: one by kind and
// name, or every object in a manifest by file.
// Useful for triggering state transitions (e.g. updating a field to drive a state machine).
type KubectlPatch struct {
	// File is a manifest whose objects are all patched, each in the namespace
	// it declares. Relative paths resolve from the directory of the file that
	// declares the step. Mutually exclusive with kind and name.
	// Generates: kubectl patch -f <file>
	File string `yaml:"file,omitempty"`
	// Kind is the Kubernetes resource kind (e.g. Deployment, MyResource).
	Kind string `yaml:"kind,omitempty"`
	// Name is the resource name.
	Name string `yaml:"name,omitempty"`
	// Namespace to target with kind and name. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Type is the patch strategy: merge (default), strategic, or json.
	Type string `yaml:"type,omitempty"` // merge | strategic | json
	// Patch is the patch content as a YAML or JSON string. Mutually exclusive
	// with patchFile.
	Patch string `yaml:"patch,omitempty"`
	// PatchFile is a file holding the patch content, resolved like file.
	// Generates: kubectl patch ... --patch-file <patchFile>
	PatchFile string `yaml:"patchFile,omitempty"`
}

// UnmarshalYAML accepts a plain string as shorthand for a file that is both
// the target and the patch: one partial object naming the resource and the
// fields to change, applied as a merge patch.
func (p *KubectlPatch) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		p.File, p.PatchFile = value.Value, value.Value
		return nil
	}
	type plain KubectlPatch
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return strictUnmarshal(raw, (*plain)(p))
}

// JSONSchemaExtend accepts a path beside the entry, as UnmarshalYAML does.
func (KubectlPatch) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s, schemagen.Str("A file that is both the target and the merge patch: one partial object (shorthand for file and patchFile)"))
}

// KubectlEvents lists Kubernetes events for a specific resource and asserts
// the output. Useful for verifying that the operator emitted expected events
// (e.g. Reconciled, BackOff) or that no error events occurred.
type KubectlEvents struct {
	// Kind is the Kubernetes resource kind to filter events for.
	Kind string `yaml:"kind"`
	// Name is the resource name.
	Name string `yaml:"name"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
}

// KubectlAuth checks permissions via kubectl auth can-i and asserts the result.
// Useful for verifying that the operator created the correct RBAC resources.
type KubectlAuth struct {
	// Verb is the action to check (e.g. get, list, create, delete).
	Verb string `yaml:"verb"`
	// Resource is the Kubernetes resource type (e.g. pods, deployments, secrets).
	Resource string `yaml:"resource"`
	// Namespace scopes the check. Omit for cluster-scoped checks.
	Namespace string `yaml:"namespace,omitempty"`
	// As is the user or service account to impersonate.
	// Use the full service account form: system:serviceaccount:<ns>:<name>
	As string `yaml:"as,omitempty"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
}

// KubectlCp copies a file out of a running container and asserts its content.
// Resolves the pod by name or label selector, copies the file to a temp path,
// reads it, applies assertions, and cleans up.
type KubectlCp struct {
	// Name is the pod name. Use LabelSelector to match by label instead.
	Name string `yaml:"name,omitempty"`
	// LabelSelector selects the pod by label (e.g. "app=my-service").
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Container name. Defaults to the first container.
	Container string `yaml:"container,omitempty"`
	// Src is the path inside the container to copy from.
	Src string `yaml:"src"`
	// assert.Extractor selects one value from a command's output before assertions run.
	assert.Extractor `yaml:",inline"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
}

// KubectlTop queries live CPU and memory usage via kubectl top and asserts
// the output. Requires metrics-server in the cluster; the runner installs it
// automatically (via Helm) when any top entry is present.
type KubectlTop struct {
	// Kind is the resource type to query: "pod" or "node".
	Kind string `yaml:"kind"`
	// Name is the pod or node name. Omit to list all.
	Name string `yaml:"name,omitempty"`
	// LabelSelector filters pods by label (e.g. "app=my-service"). Pods only.
	LabelSelector string `yaml:"labelSelector,omitempty"`
	// Namespace to query. Applies to pods only. Default: "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Containers shows per-container metrics (--containers). Pods only.
	Containers bool `yaml:"containers,omitempty"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
}

// KubectlLeaderElection resolves the target pod by reading the current holder
// from a Kubernetes Lease object. Used by port-forward, logs, delete, and exec
// to pin commands to the elected leader rather than a random replica.
type KubectlLeaderElection struct {
	// Lease is the name of the Kubernetes Lease object that tracks leader election.
	Lease string `yaml:"lease"`
	// Namespace of the lease. Defaults to the port-forward namespace.
	Namespace string `yaml:"namespace,omitempty"`
}

// KubectlPortForward opens a port-forward to a service or pod, makes an HTTP
// request via curl, and asserts the response. The port-forward lifecycle
// (background process, wait for port, cleanup) is handled by the runner.
// EnsureCurl is called automatically when any entry declares a path.
type KubectlPortForward struct {
	// Service is the service name to port-forward to. Optional when leaderElection is set.
	Service string `yaml:"service,omitempty"`
	// Pod is the pod name to port-forward to. Optional when leaderElection is set.
	Pod string `yaml:"pod,omitempty"`
	// Namespace to look in. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// LeaderElection resolves the port-forward target from a Kubernetes Lease object.
	// When set, service and pod are ignored — the current lease holder is used instead.
	// The lease namespace defaults to the port-forward namespace.
	LeaderElection *KubectlLeaderElection `yaml:"leaderElection,omitempty"`
	// Port is the service or pod port to forward.
	Port int `yaml:"port"`
	// Path is the HTTP path to request after the port-forward is ready.
	// Default "/" when the check asserts anything or sends a request
	// (statusCode, assertions, an extractor, method, headers, a body);
	// without either, only the forward is checked, for a port that doesn't
	// speak HTTP.
	Path string `yaml:"path,omitempty"`
	// Method is the HTTP method. Defaults to GET.
	Method string `yaml:"method,omitempty"`
	// Headers are added to the request, e.g. Authorization for a token-gated
	// endpoint like the Gateway API. ${NAME} in a value is expanded from the
	// env (same syntax as gateway.api.auth.tokens.token), so a CI secret
	// never needs to be written into the YAML file.
	//
	//	headers:
	//	  Authorization: "Bearer ${INRUN_CI_TOKEN}"
	Headers map[string]string `yaml:"headers,omitempty"`
	// Body is sent as the request body for methods that accept one (POST,
	// PUT, PATCH). Ignored for GET/HEAD/DELETE. ${NAME} is expanded, as in
	// Headers.
	Body string `yaml:"body,omitempty"`
	// BodyFile is a path to read the request body from. Relative paths
	// resolve against the E2E file's directory; ${NAME} in its content is
	// expanded. Content-Type follows the extension (.json, .yaml) unless a
	// header sets it. Mutually exclusive with Body.
	BodyFile string `yaml:"bodyFile,omitempty"`
	// Wait is an optional duration to sleep after the port-forward is ready
	// but before the curl request is made. Useful when the endpoint needs a
	// moment to stabilise after the connection is established.
	// Must be a valid Go duration string (e.g. "2s", "5s").
	Wait string `yaml:"wait,omitempty"`
	// assert.Extractor selects one value from a command's output before assertions run.
	assert.Extractor `yaml:",inline"`
	// Assert checks the output: the comparisons conditions use.
	Assert *assert.Assertions `yaml:"assert,omitempty"`
	// assert.TextAssertion shorthands — equals:, contains:, prefix:, … — applied to
	// the output directly, without nesting under assert:.
	assert.TextAssertion `yaml:",inline"`
	// StatusCode asserts the HTTP response status code. When set, the response
	// body is discarded and only the numeric status is checked (e.g. 404, 200).
	// Cannot be combined with JQ, YQ, Equals, or OutputContains.
	StatusCode int `yaml:"statusCode,omitempty"`
}

// KubectlRestart triggers a rollout restart of a Deployment, StatefulSet, or DaemonSet.
type KubectlRestart struct {
	// Kind is the resource kind: Deployment, StatefulSet, or DaemonSet.
	Kind string `yaml:"kind"`
	// Name is the resource name.
	Name string `yaml:"name"`
	// Namespace to target. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Ready waits for the rollout to complete after the restart. Defaults to true.
	Ready *bool `yaml:"ready,omitempty"`
}

// KubectlScale sets the replica count on a Deployment, StatefulSet, or ReplicaSet.
type KubectlScale struct {
	// Kind is the resource kind: Deployment, StatefulSet, or ReplicaSet.
	Kind string `yaml:"kind"`
	// Name is the resource name.
	Name string `yaml:"name"`
	// Namespace to target. Defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
	// Replicas is the desired replica count.
	Replicas int `yaml:"replicas"`
	// Ready waits for the rollout to complete after scaling. Defaults to true.
	Ready *bool `yaml:"ready,omitempty"`
}

// ApplyEntries are manifests to apply, in order: written as one path, a
// list of paths, or entries with their own waits.
type ApplyEntries []ApplyEntry

// UnmarshalYAML accepts one path as well as a list.
func (a *ApplyEntries) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*a = ApplyEntries{{Path: value.Value}}
		return nil
	}
	var list []ApplyEntry
	if err := value.Decode(&list); err != nil {
		return err
	}
	*a = list
	return nil
}

// JSONSchemaExtend accepts one path beside the list, as UnmarshalYAML does.
func (ApplyEntries) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s, schemagen.Str("Path to a manifest"))
}

// Commands are commands, run in order.
type Commands []Command

// HelmInstalls are charts to install, in order.
type HelmInstalls []HelmInstall

// Waits are resources to wait for.
type Waits []Wait

// ResolveEnv returns e's environment: specEnv overlaid by e's own env, then
// the process environment.
func (e Expectation) ResolveEnv(specEnv map[string]string) env.Resolver {
	return env.New(env.Vars(e.Env).Inherit(specEnv))
}

// EcosystemContext is the context a run's ecosystem connection details
// are read from: ${{ ecosystem.postgres.url }}.
const EcosystemContext = "ecosystem"

// Contexts are the ${{ name.key }} contexts a run adds to every env
// resolver, by name: the ecosystem's connection details, keyed tool.field.
type Contexts map[string]map[string]string

// Apply returns vars with each context added.
func (c Contexts) Apply(vars env.Resolver) env.Resolver {
	for name, values := range c {
		vars = vars.WithContext(name, values)
	}
	return vars
}

// Unresolved reports the references in v that vars cannot resolve: env
// names declared nowhere and unset in the process environment, and the
// context references UnresolvedContexts reports. nil when every reference
// resolves.
func Unresolved(vars env.Resolver, v any) error {
	var errs []error
	if names := env.Unknown(vars, v); len(names) > 0 {
		errs = append(errs, fmt.Errorf("unknown env %s: declare it in env: or set it in the environment",
			"${"+strings.Join(names, "}, ${")+"}"))
	}
	if err := UnresolvedContexts(vars, v); err != nil {
		errs = append(errs, err)
	}
	return oneLine(errs)
}

// UnresolvedContexts reports the ${{ name.key }} references in v into a
// context of vars that has no such key, each with the keys it has. nil when
// every one resolves.
func UnresolvedContexts(vars env.Resolver, v any) error {
	var errs []error
	for _, ref := range env.UnknownContext(vars, v) {
		name, _, _ := strings.Cut(ref, ".")
		has := "nothing is declared there"
		if keys := vars.ContextKeys(name); len(keys) > 0 {
			has = "it has " + strings.Join(keys, ", ")
		}
		errs = append(errs, fmt.Errorf("unknown ${{ %s }}: %s", ref, has))
	}
	return oneLine(errs)
}

// oneLine joins errs with "; " into one error, for a message that stays on
// one line; nil when there are none. (joinErrors is the document's list.)
func oneLine(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return errors.New(strings.Join(msgs, "; "))
}

// RequestPath is the path the check requests: Path, else "/" when the check
// asserts anything or sends a request, else "" (only the forward is
// checked).
func (p KubectlPortForward) RequestPath() string {
	switch {
	case p.Path != "":
		return p.Path
	case p.StatusCode != 0, p.Method != "", len(p.Headers) > 0, p.Body != "", p.BodyFile != "",
		p.JQ != "", p.YQ != "", p.JSONPath != "",
		!(assert.Assertions{TextAssertion: p.TextAssertion}).Empty(),
		p.Assert != nil && !p.Assert.Empty():
		return "/"
	}
	return ""
}

// Snapshot is a golden file for an object: written on the first run, then
// compared. With Update, drift rewrites the file instead of failing (the
// report says so); written as a path alone, it only compares.
type Snapshot struct {
	File   string `yaml:"file"`
	Update bool   `yaml:"update,omitempty"`
}

// UnmarshalYAML accepts the file's path alone.
func (s *Snapshot) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		s.File = value.Value
		return nil
	}
	type plain Snapshot
	raw, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return strictUnmarshal(raw, (*plain)(s))
}
