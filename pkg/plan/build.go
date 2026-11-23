package plan

import (
	"fmt"
	"sort"
	"strings"

	jigecosystem "github.com/intentd/jig/pkg/ecosystem"
	"github.com/intentd/jig/pkg/gitsource"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/resolver/env"
)

// buildPlan resolves the information needed to describe a declaration's plan.
//
// A non-nil error means the plan could not be constructed. Warnings are
// non-fatal and are returned in Result.
func buildPlan(d *declaration) (Result, error) {
	if d == nil || d.Empty() {
		return Result{}, errDeclarationEmpty
	}

	result := Result{
		Name:        d.Name,
		Description: strings.TrimSpace(d.Description),
		Summary: summary{
			Prepare:      len(d.Prepare),
			Expectations: len(d.Expect),
			Imports:      len(d.Imports),
			Aggregator:   d.IsAggregator(),
		},
	}

	mode := d.Environment.Mode()
	result.Mode = mode.String()
	if mode == modeCluster {
		result.Cluster = d.Environment.ClusterName("jig")
	}

	if e := d.Environment; e != nil {
		environment := &environmentPlan{
			Helm: len(e.Helm),
			Wait: len(e.Wait),
		}

		for _, apply := range e.Apply {
			environment.Apply = append(environment.Apply, apply.Path)
		}

		secretNames, err := ecosystemSecretNames(e.Ecosystem)
		if err != nil {
			return Result{}, fmt.Errorf("plan: %w", err)
		}

		environment.Images = buildImagePlans(d.Name, e.Images, secretNames)

		environment.Ecosystem, err = buildEcosystemPlan(e.Ecosystem)
		if err != nil {
			return Result{}, err
		}

		result.Environment = environment
	}

	result.Prepare = resolvePrepare(d)
	result.Expect = resolveExpect(d)

	if d.IsAggregator() {
		result.Summary.Prepare = 0
		result.Summary.Expectations = 0

		for _, imp := range d.Imports {
			decl, err := imp.Read(d.Dir)
			if err != nil {
				return result, fmt.Errorf("reading import %q: %w", imp.Path, err)
			}

			prepare := resolvePrepare(decl)
			expect := resolveExpect(decl)

			result.Imports = append(result.Imports, importPlan{
				Path:         imp.Path,
				FreshCluster: imp.FreshCluster,
				Wait:         imp.Wait,
				Prepare:      prepare,
				Expect:       expect,
			})

			result.Summary.Prepare += len(prepare)
			result.Summary.Expectations += len(expect)
		}
	}

	for _, guard := range d.Guards {
		result.Guards = append(result.Guards, guard.Name)
	}

	result.Warnings = append(result.Warnings, planEnvironmentWarnings(d)...)

	return result, nil
}

// resolvePrepare converts a declaration's prepare steps into their plan
// representations.
func resolvePrepare(d *declaration) []preparePlan {
	var prepare []preparePlan

	for i, step := range d.Prepare {
		name := step.Name
		if name == "" {
			name = fmt.Sprintf("prepare[%d]", i)
		}

		prepare = append(prepare, preparePlan{
			Index:  i,
			Name:   name,
			Action: action(step),
		})
	}

	return prepare
}

// resolveExpect converts a declaration's expectations into their plan
// representations, applying default values where necessary.
func resolveExpect(d *declaration) []expectationPlan {
	var expect []expectationPlan

	for _, expectation := range d.Expect {
		timeout := expectation.Timeout
		if timeout == "" {
			timeout = "60s"
		}

		after := expectation.After
		if after == "" {
			after = afterPrepare
		}

		expect = append(expect, expectationPlan{
			Name:    expectation.Name,
			After:   after,
			Timeout: timeout,
		})
	}

	return expect
}

func buildImagePlans(testName string, images cluster.Images, secretNames map[string]bool) []imagePlan {
	var result []imagePlan

	for _, img := range images.ResolveDefaultNames(testName) {
		plan := imagePlan{
			Name:       img.Name,
			Image:      img.Image,
			Operations: []string{"load"},
		}

		if img.Build != nil {
			plan.Build = &imageBuildPlan{
				Context:    img.Build.Context,
				Dockerfile: img.Build.DockerfilePath(),
			}
			plan.Operations = []string{"build", "load"}
		}

		if img.Deploy != nil {
			plan.Operations = append(plan.Operations, "deploy")
		}

		if row := img.Row; row != nil {
			plan.Row = &imageRowPlan{
				Entry: row.Entry,
				Index: row.Index,
				Name:  img.Name,
				Base:  row.Base,
			}
		}

		if git := img.Git; git != nil {
			at := "the working tree"

			switch {
			case git.Ref != "":
				at = "ref " + git.Ref
			case git.Branch != "":
				at = "branch " + git.Branch
			case gitsource.IsRemote(git.URL):
				at = "the default branch"
			}

			gitPlan := &imageGitPlan{
				URL: git.URL,
				At:  at,
			}

			if gitsource.IsRemote(git.URL) {
				gitPlan.Pull = or(git.Pull, cluster.PullAlways)
			}

			plan.Git = gitPlan
		}

		if deploy := img.Deploy; deploy != nil {
			namespace := or(deploy.Namespace, "default")

			deployPlan := &imageDeployPlan{
				Name:      deploy.Name,
				Namespace: namespace,
				Needs:     img.Needs,
			}

			for _, port := range deploy.Ports {
				deployPlan.Ports = append(deployPlan.Ports, imagePortPlan{
					Port:      port.Port,
					Exposed:   port.Exposed(),
					Forwarded: port.Forwarded(),
					Host:      port.Host,
				})
			}

			for i, init := range deploy.Init {
				deployPlan.Init = append(deployPlan.Init, imageInitPlan{
					Name:    or(init.Name, fmt.Sprintf("init-%d", i)),
					Command: init.Command,
					Image:   or(init.Image, "the app's image"),
				})
			}

			for name := range deploy.Env {
				deployPlan.Env = append(deployPlan.Env, name)
			}
			sort.Strings(deployPlan.Env)

			for _, name := range deploy.EnvFrom {
				deployPlan.EnvFrom = append(deployPlan.EnvFrom, imageEnvFromPlan{
					Name:               name,
					Namespace:          namespace,
					WrittenByEcosystem: secretNames[namespace+"/"+name],
				})
			}

			plan.Deploy = deployPlan
		}

		result = append(result, plan)
	}

	return result
}

func buildEcosystemPlan(e *ecosystem) (*ecosystemPlan, error) {
	if e == nil {
		return nil, nil
	}

	p, err := jigecosystem.New(e, jigecosystem.Placeholder)
	if err != nil {
		return nil, fmt.Errorf("build ecosystem plan: %w", err)
	}
	if p == nil {
		return nil, nil
	}

	namespace := e.Namespace
	if namespace == "" {
		namespace = jigecosystem.DefaultNamespace
	}

	result := &ecosystemPlan{
		Namespace: namespace,
	}

	for _, service := range jigecosystem.Services {
		connection, ok := p.Conns[service]
		if !ok {
			continue
		}

		address := connection["bootstrap"]
		if address == "" {
			address = connection["host"] + ":" + connection["port"]
		}

		result.Services = append(result.Services, ecosystemServicePlan{
			Name:    service,
			Address: address,
		})
	}

	for _, operator := range p.Operators {
		name := operator.Name
		if operator.Chart != nil && operator.Chart.Version != "" {
			name += " " + operator.Chart.Version
		}
		result.Operators = append(result.Operators, name)
	}

	for _, secret := range p.Outputs {
		keys := make([]string, 0, len(secret.Data))
		for key := range secret.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		result.Secrets = append(result.Secrets, ecosystemSecretPlan{
			Name:      secret.Name,
			Namespace: secret.Namespace,
			Keys:      keys,
		})
	}

	return result, nil
}

func planEnvironmentWarnings(d *declaration) warnings {
	var warnings warnings

	declared := make(map[string]string)
	for key, value := range d.Env {
		declared[key] = value
	}

	for name := range d.Captures() {
		declared[name] = "(captured)"
	}

	for _, expectation := range d.Expect {
		unknown := env.Unknown(expectation.ResolveEnv(declared), expectation)
		if len(unknown) == 0 {
			continue
		}

		warnings = append(warnings, warning{
			Code: "undeclared-environment-variable",
			Message: fmt.Sprintf(
				"%q uses %s: not declared in env and not set here",
				expectation.Name,
				strings.Join(unknown, ", "),
			),
		})
	}

	for _, expectation := range d.Expect {
		if expectation.Kubectl == nil {
			continue
		}

		for _, get := range expectation.Kubectl.Get {
			if get.Snapshot == nil || !get.Snapshot.Update {
				continue
			}

			warnings = append(warnings, warning{
				Code: "snapshot-update-enabled",
				Message: fmt.Sprintf(
					"%q: snapshot %s has update: true, so it rewrites itself instead of failing",
					expectation.Name,
					get.Snapshot.File,
				),
			})
		}
	}

	return warnings
}
