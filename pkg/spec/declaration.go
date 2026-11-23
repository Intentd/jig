// Package spec is the jig document: the environment, the prepare
// steps, the expectations and what runs on failure and at the end, as a
// jig.yaml declares them, with FromFile to read one and Validate to check it.
package spec

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/intentd/kel/core"
	"github.com/intentd/tools/assert"
	"github.com/intentd/tools/resolver"
	"github.com/intentd/tools/resolver/env"
	"github.com/intentd/tools/schemagen"
	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"
)

// Declaration is a jig: a jig.yaml. It has no apiVersion or kind; the keys are
// the document.
type Declaration struct {
	// Name is the jig's name, shown in the report.
	Name string `yaml:"name"`
	// Description says what the test checks; shown in the report.
	Description string `yaml:"description,omitempty"`

	// Env are variables for the run: ${NAME} in values expands to them, then
	// to the process environment.
	Env env.Vars `yaml:"env,omitempty" env:"-"`
	// Functions are named template expressions, callable from every template.
	Functions Functions `yaml:"functions,omitempty"`

	// Environment is the cluster and the infrastructure the workload depends
	// on: images, early manifests and charts, the ecosystem.
	Environment *Environment `yaml:"environment,omitempty"`
	// Prepare are the steps that put the thing under test into the cluster
	// and act on it, run in order.
	Prepare Steps `yaml:"prepare,omitempty"`
	// Expect are the checks, each run after the environment, after a named
	// step, or after the last step.
	Expect Expectations `yaml:"expect,omitempty"`

	// OnFailure runs once when the run fails; Stop there ends the run at the
	// first failing expectation that doesn't set its own.
	OnFailure *OnFailure `yaml:"onFailure,omitempty"`
	// Finally runs once at the end of every run.
	Finally *Finally `yaml:"finally,omitempty"`

	// Imports are other jigs run after this one.
	Imports []Import `yaml:"imports,omitempty"`

	// Guards are checks on the whole run, made at its end and reported
	// like expectations: no Warning events, no restarts, no errors in logs.
	Guards []Guard `yaml:"guards,omitempty"`

	// Dir is the directory the file was read from: relative paths resolve
	// against it. Set by FromFile.
	Dir string `yaml:"-"`
	// Data is what expectations' when:/or: conditions read, such as the
	// object under test. Set by the caller, not read from the file.
	Data map[string]any `yaml:"-"`
}

// IsAggregator reports whether the jig at path only imports: it has imports
// and no prepare steps and no expectations. A suite is an aggregator: it
// lends its cluster to the jigs it imports rather than running anything
// itself.
//
// It reads the file, so it returns false when the file cannot be read or has
// a shape problem. The caller that runs the import reports the error; this
// is a query about a jig that is expected to be readable, not a validator.
func IsAggregator(path string) bool {
	d, err := Read(path)
	if err != nil {
		return false // run reports it
	}
	return d.IsAggregator()
}

// IsAggregator reports whether d only imports: imports present, and no
// prepare steps and no expectations. An aggregator is a suite — it lends its
// cluster to the jigs it imports rather than running anything itself.
func (d *Declaration) IsAggregator() bool {
	return len(d.Imports) > 0 && len(d.Prepare) == 0 && len(d.Expect) == 0
}

// Functions are named template expressions.
type Functions struct {
	Definitions []Function `yaml:"definitions,omitempty"`
}

// Functions returns the definitions as the template resolver calls them.
func (f Functions) Functions() []resolver.Function {
	out := make([]resolver.Function, 0, len(f.Definitions))
	for _, d := range f.Definitions {
		out = append(out, resolver.Function{Name: d.Name, Expression: d.Expression})
	}
	return out
}

// Function is one named expression: {{ name }} in any template evaluates it.
type Function struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Expression  string `yaml:"expression"`
}

// Steps are prepare steps, run in order.
type Steps []Step

// Step is one prepare step: exactly one action, then its waits and restarts.
type Step struct {
	// Name is how expectations refer to the step (after: <name>).
	Name string `yaml:"name,omitempty"`

	// Apply server-side applies manifests, in order: a path, a list of
	// paths, or entries with their own waits.
	Apply ApplyEntries `yaml:"apply,omitempty"`
	// Helm runs helm upgrade --install: a first run installs, a later one
	// upgrades.
	Helm *HelmInstall `yaml:"helm,omitempty"`
	// Delete deletes what manifests declare: a path or a list of paths. It
	// doesn't wait for finalizers; the expectations after the step wait for
	// what the deletion brings about.
	Delete Paths `yaml:"delete,omitempty"`
	// Commands run in order; a non-zero exit fails the step.
	Commands Commands `yaml:"commands,omitempty"`

	// Wait waits for resources after the action.
	Wait Waits `yaml:"wait,omitempty"`
	// Restart rolls out workloads after the action and waits for the
	// rollout: for a workload reading a ConfigMap or Secret the step changed,
	// which neither apply nor helm upgrade restarts.
	Restart Restarts `yaml:"restart,omitempty"`

	// Keep leaves what the step applied, and the Helm release it installed,
	// for the imports after this test when it runs as an import on its
	// parent's cluster: later imports upgrade the release, and the parent
	// removes it all after its last import. Run on its own, the test tears
	// the step down as usual. Default false: an import undoes everything it
	// did.
	Keep bool `yaml:"keep,omitempty"`
}

// Restarts are workloads to roll out.
type Restarts []Restart

// Paths are manifest paths: written as one path or a list.
type Paths []string

// UnmarshalYAML accepts one path as well as a list.
func (p *Paths) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*p = Paths{value.Value}
		return nil
	}
	var list []string
	if err := value.Decode(&list); err != nil {
		return err
	}
	*p = list
	return nil
}

// JSONSchemaExtend accepts one path beside the list, as UnmarshalYAML does.
func (Paths) JSONSchemaExtend(s *jsonschema.Schema) {
	schemagen.Accept(s, schemagen.Str("Path to a manifest"))
}

// Restart is a workload to roll out.
type Restart struct {
	// Kind is Deployment, StatefulSet or DaemonSet.
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
	// Namespace defaults to "default".
	Namespace string `yaml:"namespace,omitempty"`
}

// Where an expectation runs, besides after a named step.
const (
	// AfterEnvironment runs once the environment is ready, before prepare.
	AfterEnvironment After = "environment"
	// AfterPrepare runs once every prepare step has run: the default.
	AfterPrepare After = "prepare"
)

// actions are the step's set actions, by key.
func (s Step) actions() []string {
	var set []string
	if len(s.Apply) > 0 {
		set = append(set, "apply")
	}
	if s.Helm != nil {
		set = append(set, "helm")
	}
	if len(s.Delete) > 0 {
		set = append(set, "delete")
	}
	if len(s.Commands) > 0 {
		set = append(set, "commands")
	}
	return set
}

// Read parses the jig at path, strictly, with its includes expanded and
// its directory recorded for relative paths. It does not validate: FromFile
// adds the shape checks, and the check package the whole check.
func Read(path string) (*Declaration, error) {
	data, err := readLocal(path)
	if err != nil {
		return nil, err
	}
	var s Declaration
	if err := strictUnmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.Dir = filepath.Dir(path)
	if err := s.Environment.ExpandIncludes(s.Dir); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.Environment.ExpandForEach(s.Functions.Functions()); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.Prepare.ExpandIncludes(s.Dir); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Expect, err = s.Expect.ExpandIncludes(s.Dir); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// FromFile reads the jig at path and checks its shape (Validate). An
// error lists every problem.
func FromFile(path string) (*Declaration, error) {
	s, err := Read(path)
	if err != nil {
		return nil, err
	}
	if problems := Validate(s); len(problems) > 0 {
		return nil, Problems(path, problems)
	}
	return s, nil
}

// Problems is the error for a test's problems: one per line, under its path.
func Problems(path string, problems []error) error {
	return fmt.Errorf("%s: %w", path, joinErrors(problems))
}

// Captures returns the names capture: saves values under, anywhere in d.
func (d *Declaration) Captures() map[string]bool {
	out := map[string]bool{}

	if d.Empty() {
		return out
	}

	add := func(assertions assert.JSONAssertions) {
		for _, assertion := range assertions {
			if assertion.Capture != "" {
				out[assertion.Capture] = true
			}
		}
	}

	for _, expectation := range d.Expect {
		for _, command := range expectation.Commands {
			add(command.JSON)
		}

		if expectation.Kubectl != nil {
			for _, get := range expectation.Kubectl.Get {
				add(get.JSON)
			}
		}
	}

	return out
}

var (
	ErrDeclarationEmpty  = errors.New("declaration is empty")
	ErrsDeclarationEmpty = []error{ErrDeclarationEmpty}
)

// Empty reports whether the declaration is nil.
func (d *Declaration) Empty() bool {
	return d == nil
}

// Validate orchestrates declaration validation.
// A nil declaration is the only case handled directly here.
func Validate(d *Declaration) []error {
	if d.Empty() {
		return ErrsDeclarationEmpty
	}

	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	// Run each validation independently.
	validateEnvironment(d, add)
	steps := validatePrepare(d, add)
	validateExpectations(d, steps, add)
	validateImports(d, add)

	return errs
}

// addError collects validation errors.
type addError func(format string, args ...any)

// validateEnvironment checks environment configuration.
func validateEnvironment(d *Declaration, add addError) {
	if d.Environment == nil {
		return
	}

	if p := d.Environment.Cluster.Provider; p != "" && !p.IsValid() {
		add("environment.cluster.provider %q: one of %v", p, ValidClusterProviders())
	}
}

// validatePrepare checks prepare steps and returns their names
// so expectation validation can verify its dependencies.
func validatePrepare(d *Declaration, add addError) map[string]bool {
	steps := map[string]bool{}

	for i, st := range d.Prepare {
		at := fmt.Sprintf("prepare[%d]", i)

		if st.Name != "" {
			at = fmt.Sprintf("prepare[%d] (%s)", i, st.Name)

			if st.Name == string(AfterEnvironment) || st.Name == string(AfterPrepare) {
				add("%s: %q is reserved for after:", at, st.Name)
			}

			if steps[st.Name] {
				add("%s: the name is used by an earlier step", at)
			}

			steps[st.Name] = true
		}

		// Every prepare step must contain exactly one action.
		switch acts := st.actions(); len(acts) {
		case 0:
			add("%s: set one of apply, helm, delete or commands", at)
		case 1:
			// Exactly one action is valid.
		default:
			add("%s: one action per step, got %v", at, acts)
		}

		// Validate Helm configuration when present.
		if st.Helm != nil {
			if err := st.Helm.Validate(); err != nil {
				add("%s.helm: %v", at, err)
			}
		}

		// Keep requires apply or helm to preserve resources.
		if st.Keep && len(st.Apply) == 0 && st.Helm == nil {
			add("%s: keep leaves what apply or helm did for later imports; this step has neither", at)
		}

		// Validate restart targets.
		for j, r := range st.Restart {
			switch r.Kind {
			case "Deployment", "StatefulSet", "DaemonSet":
			default:
				add("%s.restart[%d]: kind %q: Deployment, StatefulSet or DaemonSet", at, j, r.Kind)
			}

			if r.Name == "" {
				add("%s.restart[%d]: name is required", at, j)
			}
		}
	}

	return steps
}

// validateExpectations checks expectation names, dependencies and durations.
func validateExpectations(d *Declaration, steps map[string]bool, add addError) {
	names := map[string]bool{}

	for i, e := range d.Expect {
		at := fmt.Sprintf("expect[%d]", i)

		// Expectation names are required and must be unique.
		if e.Name == "" {
			add("%s: name is required", at)
		} else {
			at = fmt.Sprintf("expect[%d] (%s)", i, e.Name)

			if names[e.Name] {
				add("%s: the name is used by an earlier expectation", at)
			}

			names[e.Name] = true
		}

		// Expectations can run after the environment, prepare,
		// or a specifically named prepare step.
		switch a := string(e.After); {
		case a == "",
			a == string(AfterEnvironment),
			a == string(AfterPrepare),
			steps[a]:
		default:
			add("%s: after %q: environment, prepare or the name of a prepare step", at, a)
		}

		// Validate expectation timeout and wait durations.
		for field, duration := range map[string]string{
			"timeout": e.Timeout,
			"wait":    e.Wait,
		} {
			if duration == "" {
				continue
			}

			if _, err := core.ParseTimeDuration(duration); err != nil {
				add("%s: %s %q is not a duration: %v", at, field, duration, err)
			}
		}
	}
}

// validateImports checks import paths, cluster configuration and wait durations.
func validateImports(d *Declaration, add addError) {
	for i, imp := range d.Imports {
		if imp.Path == "" {
			add("imports[%d]: path is required", i)
		}

		if imp.FreshCluster && imp.Context != "" {
			add("imports[%d]: set freshCluster or context, not both", i)
		}

		if imp.Wait != "" {
			if _, err := core.ParseTimeDuration(imp.Wait); err != nil {
				add("imports[%d]: wait %q is not a duration: %v", i, imp.Wait, err)
			}
		}
	}
}

// joinErrors joins problems one per line.
func joinErrors(problems []error) error {
	msg := ""
	for i, p := range problems {
		if i > 0 {
			msg += "\n"
		}
		msg += "  - " + p.Error()
	}
	return fmt.Errorf("invalid jig:\n%s", msg)
}
