package runner

import (
	"context"
	"fmt"
	"reflect"

	"github.com/intentd/jig/pkg/ecosystem"
	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/jig/pkg/post"
	"github.com/intentd/jig/pkg/spec"
)

// planEcosystem works out the ecosystem's plan, first: its connection
// details are known before anything is installed, so every resolver reads
// them (${{ ecosystem.postgres.url }}, {{ .ecosystem.postgres.url }}), and
// the Secrets and the chart's values come from the same plan, with the same
// passwords. The context is there even with no ecosystem declared, so a
// reference into it fails as unknown instead of staying as written.
func (r *runner) planEcosystem() error {
	var declared *spec.Ecosystem
	if r.spec.Environment != nil {
		declared = r.spec.Environment.Ecosystem
	}
	p, err := r.suitePlan(declared)
	if err != nil {
		return err
	}
	r.ecosystem = p
	r.contexts = spec.Contexts{spec.EcosystemContext: p.Context()}
	r.templateData = map[string]any{spec.EcosystemContext: p.Data()}
	return nil
}

// installEcosystem installs the plan, after the images load and before
// anything is deployed: the operators, the Secrets, the data-services
// chart, then each instance's readiness, so a Deployment comes after the
// Secrets it reads and finds its services there. Each step is undone at
// teardown, in reverse; a namespace or an operator only when jig made it.
// Under --dry-run=server nothing is installed: helm and the client would
// write for real.
func (r *runner) installEcosystem(ctx context.Context) error {
	if r.ecosystem == nil {
		return nil
	}
	if kubectl.DryRun() {
		fmt.Println("→ Ecosystem: skipped under --dry-run=server")
		return nil
	}
	// On a shared cluster the suite's ecosystem is installed once, and
	// lasts until the parent's last import.
	undos := &r.undos
	if eco := r.o.suiteEcosystem; eco != nil && r.o.shared {
		where := r.effectiveContext(ctx)
		if eco.installedOn[where] {
			fmt.Println("→ Ecosystem: installed for the suite already")
			return nil
		}
		eco.installedOn[where] = true
		if r.o.parentUndos != nil {
			undos = r.o.parentUndos
		}
	}
	fmt.Println("→ Installing the ecosystem...")
	done, err := r.ecosystem.Install(ctx, r.cs, ecosystem.InstallOptions{Chart: r.o.EcosystemChart})
	for _, u := range done {
		*undos = append(*undos, post.Undo{Label: u.Label, Do: u.Do})
	}
	if err != nil {
		return fmt.Errorf("ecosystem: %w", err)
	}
	return nil
}

// suiteEcosystem is the ecosystem a suite's imports share on a shared
// cluster: the declaration it was planned from, the plan (one set of
// passwords), and the contexts it is installed on.
type suiteEcosystem struct {
	declared    *spec.Ecosystem
	plan        *ecosystem.Plan
	installedOn map[string]bool
}

// suitePlan is the plan for declared: the suite's, for an import on a
// shared cluster that declares the same ecosystem (or the first to declare
// one), else a plan of its own. An import declaring another ecosystem on a
// shared cluster is an error: one instance per service, with fixed names.
func (r *runner) suitePlan(declared *spec.Ecosystem) (*ecosystem.Plan, error) {
	eco := r.o.suiteEcosystem
	if eco == nil || !r.o.shared || declared == nil {
		return ecosystem.New(declared, ecosystem.Password)
	}
	switch {
	case eco.plan == nil:
		p, err := ecosystem.New(declared, ecosystem.Password)
		if err != nil {
			return nil, err
		}
		eco.declared, eco.plan = declared, p
		return p, nil
	case reflect.DeepEqual(eco.declared, declared):
		return eco.plan, nil
	}
	return nil, fmt.Errorf("this import declares another environment.ecosystem than the suite's, on the same cluster: one instance per service, with fixed names; give it freshCluster: true")
}
