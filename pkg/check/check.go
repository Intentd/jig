// Package check is the whole check of a jig, offline: its shape (spec),
// what a test without a cluster can't do, the files it names, its kubectl
// blocks and external calls, its side effects, and its images. jig.Validate,
// jig.FromFile and the runner's import loader use it.
package check

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/intentd/jig/pkg/ecosystem"
	"github.com/intentd/jig/pkg/expect"
	"github.com/intentd/jig/pkg/gitsource"
	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/jig/pkg/spec"
	kelecosystem "github.com/intentd/kel/ecosystem"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/fileio"
	"github.com/intentd/tools/resolver/env"
	"github.com/intentd/tools/structured"
)

// FromFile reads the jig at path and checks it. An error lists every
// problem.
func FromFile(path string) (*spec.Declaration, error) {
	d, err := spec.Read(path)
	if err != nil {
		return nil, err
	}
	if problems := Validate(d); len(problems) > 0 {
		return nil, spec.Problems(path, problems)
	}
	return d, nil
}

// Validate returns every problem with the jig t declares. Relative paths
// resolve against t.Dir. Templates are checked against KEL's functions and
// the jig's own.
func Validate(d *spec.Declaration) []error { return ValidateWith(d, nil) }

// ValidateWith is Validate for a caller that runs with its own template
// functions (Options.Functions): templates are checked against funcs (KEL's
// when nil) and the jig's own.
func ValidateWith(d *spec.Declaration, funcs template.FuncMap) []error {
	if d.Empty() {
		return spec.ErrsDeclarationEmpty
	}
	errs := spec.Validate(d)
	errs = append(errs, noCluster(d)...)
	errs = append(errs, paths(d)...)
	errs = append(errs, kubectl.Validate(d.Expect, d.Dir)...)
	errs = append(errs, expect.ValidateExternal(d.Expect)...)
	if f := d.OnFailure; f != nil {
		errs = append(errs, block("onFailure", &f.SideEffects, f.Empty(), d.Dir)...)
	}
	if f := d.Finally; f != nil {
		errs = append(errs, block("finally", &f.SideEffects, f.Empty(), d.Dir)...)
	}
	errs = append(errs, images(d)...)
	errs = append(errs, dataServices(d)...)
	errs = append(errs, contextRefs(d)...)
	errs = append(errs, expressions(d, funcs)...)
	errs = append(errs, extras(d)...)
	return errs
}

// block checks the jig's onFailure or finally.
func block(name string, f *spec.SideEffects, empty bool, dir string) []error {
	var errs []error
	if empty {
		errs = append(errs, fmt.Errorf("%s: block is empty", name))
	}
	errs = append(errs, kubectl.ValidateBlock(name, f.Kubectl, dir, true)...)
	return append(errs, expect.ValidateExternalCalls(name, f.External)...)
}

// noCluster reports what a test without a cluster declares but can't do.
func noCluster(d *spec.Declaration) []error {
	if d.Empty() {
		return spec.ErrsDeclarationEmpty
	}

	if d.Environment.Provider() != spec.ProviderNone {
		return nil
	}
	var errs []error
	if e := d.Environment; e != nil && (len(e.Apply) > 0 || len(e.Helm) > 0 || len(e.Wait) > 0 || len(e.Images) > 0) {
		errs = append(errs, fmt.Errorf("environment: apply, helm, wait and images need a cluster; cluster.provider is none"))
	}
	for i, st := range d.Prepare {
		if len(st.Apply) > 0 || st.Helm != nil || len(st.Delete) > 0 || len(st.Wait) > 0 || len(st.Restart) > 0 {
			errs = append(errs, fmt.Errorf("prepare[%d] %q: only commands run without a cluster; cluster.provider is none", i, st.Name))
		}
	}
	for i, e := range d.Expect {
		switch {
		case e.Kubectl != nil:
			errs = append(errs, fmt.Errorf("expect[%d] %q: kubectl needs a cluster; cluster.provider is none", i, e.Name))
		case len(e.Resources) > 0:
			errs = append(errs, fmt.Errorf("expect[%d] %q: resources need a cluster; cluster.provider is none", i, e.Name))
		case len(e.External) > 0:
			errs = append(errs, fmt.Errorf("expect[%d] %q: external needs a cluster; cluster.provider is none", i, e.Name))
		}
	}
	return errs
}

// paths reports every local file the test names that is not there, resolved
// as the run resolves it. URLs, templated paths, repository charts and
// generated files are left to the run.
func paths(d *spec.Declaration) []error {
	var errs []error
	check := func(field, ref string) {
		path, ok := fileio.LocalRef(d.Dir, ref)
		if !ok {
			return
		}
		if err := fileio.CheckLocal(path); err != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", field, ref, err))
		}
	}
	applies := func(at string, entries spec.ApplyEntries) {
		for i, a := range entries {
			if !a.Generated {
				check(fmt.Sprintf("%s.apply[%d]", at, i), a.Path)
			}
		}
	}
	helm := func(at string, h spec.HelmInstall) {
		if h.IsLocalChart() {
			check(at+".chart", h.Chart)
		}
		for j, f := range h.ValueFiles {
			check(fmt.Sprintf("%s.valueFiles[%d]", at, j), f)
		}
	}

	if e := d.Environment; e != nil {
		// Resolved once, against the absolute directory: check resolves
		// relative paths against t.Dir again.
		absDir, err := filepath.Abs(d.Dir)
		if err != nil {
			absDir = d.Dir
		}
		for i, img := range e.Images {
			if img.Build == nil {
				continue
			}
			base := absDir
			if g := img.Git; g != nil {
				// A remote checkout isn't there offline, nor is a local
				// one at another branch or ref: only the working tree as
				// it is can be looked at.
				if gitsource.IsRemote(g.URL) || g.Branch != "" || g.Ref != "" {
					continue
				}
				base = gitsource.LocalPath(absDir, g.URL)
			}
			// The dockerfile is relative to the context, as in Compose. The
			// context is shown as written, unless it is inside a repository.
			b := img.Build.ResolvePaths(base)
			context := img.Build.Context
			if img.Git != nil {
				context = b.Context
			}
			check(fmt.Sprintf("environment.images[%d].build.context", i), context)
			check(fmt.Sprintf("environment.images[%d].build.dockerfile", i), b.Dockerfile)
		}
		applies("environment", e.Apply)
		for i, h := range e.Helm {
			helm(fmt.Sprintf("environment.helm[%d]", i), h)
		}
	}
	for i, st := range d.Prepare {
		at := fmt.Sprintf("prepare[%d]", i)
		applies(at, st.Apply)
		if st.Helm != nil {
			helm(at+".helm", *st.Helm)
		}
		for j, p := range st.Delete {
			check(fmt.Sprintf("%s.delete[%d]", at, j), p)
		}
	}
	return errs
}

// dataServices reports what the ecosystem declares but can't provide: a
// service export or define names that isn't declared, a reference that isn't
// service.field or names a field the service doesn't have. The plan is worked
// out as a run would, with a placeholder for the passwords.
func dataServices(d *spec.Declaration) []error {
	if d.Environment == nil || d.Environment.Ecosystem == nil {
		return nil
	}
	if _, err := ecosystem.New(d.Environment.Ecosystem, func() string { return "" }); err != nil {
		return []error{fmt.Errorf("environment.ecosystem.%w", err)}
	}
	return nil
}

// contextRefs reports each ${{ ecosystem.… }} reference, anywhere in the
// test, that the ecosystem can't resolve: a service not declared, or a field
// it doesn't have. The plan is worked out as a run would, with placeholder
// passwords, so every reference resolves the way it will.
func contextRefs(d *spec.Declaration) []error {
	var declared *spec.Ecosystem
	if d.Environment != nil {
		declared = d.Environment.Ecosystem
	}
	p, err := ecosystem.New(declared, ecosystem.Placeholder)
	if err != nil {
		return nil // dataServices reports it
	}
	vars := spec.Contexts{spec.EcosystemContext: p.Context()}.Apply(env.New(nil))
	if err := spec.UnresolvedContexts(vars, *d); err != nil {
		return []error{err}
	}
	return nil
}

// images reports image references and generated Deployments that would fail
// at run time.
func images(decl *spec.Declaration) []error {
	if decl.Environment == nil {
		return nil
	}
	var errs []error
	deployments := map[string]int{}
	builds := map[string]int{}
	for i, img := range decl.Environment.Images.ResolveDefaultNames(decl.Name) {
		if img.Build == nil {
			continue
		}
		if prev, dup := builds[img.Image]; dup {
			errs = append(errs, fmt.Errorf("environment.images[%d] builds %q, which images[%d] builds too: build it once and use the reference in the other", i, img.Image, prev))
			continue
		}
		builds[img.Image] = i
	}
	for i, img := range decl.Environment.Images {
		at := fmt.Sprintf("environment.images[%d]", i)
		if img.Image != "" {
			if !kelecosystem.IsValidImageRef(img.Image) {
				errs = append(errs, fmt.Errorf("%s.image %q is invalid", at, img.Image))
			}
		} else if img.Build == nil {
			errs = append(errs, fmt.Errorf("%s: set image or build", at))
		}
		errs = append(errs, gitSource(at, img, decl.Dir)...)
		d := img.Deploy
		if d == nil {
			continue
		}
		name := d.Name
		if name == "" {
			name = img.Name
		}
		if name == "" {
			name = img.DefaultDeployName()
		}
		for j, s := range d.EnvFrom {
			if problems := validation.IsDNS1123Subdomain(s); len(problems) > 0 && !strings.Contains(s, "${") {
				errs = append(errs, fmt.Errorf("%s.deploy.envFrom[%d] %q is not a Secret name: %s", at, j, s, strings.Join(problems, ", ")))
			}
		}
		namespace := d.Namespace
		if namespace == "" {
			namespace = "default"
		}
		if name != "" {
			if problems := validation.IsDNS1123Label(name); len(problems) > 0 {
				errs = append(errs, fmt.Errorf("%s.deploy.name %q is invalid: %s", at, name, strings.Join(problems, ", ")))
			}
			key := namespace + "/" + name
			if prev, dup := deployments[key]; dup {
				errs = append(errs, fmt.Errorf("%s.deploy duplicates deployment %q from images[%d]", at, key, prev))
			} else {
				deployments[key] = i
			}
		}
		if problems := validation.IsDNS1123Label(namespace); len(problems) > 0 {
			errs = append(errs, fmt.Errorf("%s.deploy.namespace %q is invalid: %s", at, namespace, strings.Join(problems, ", ")))
		}
		if len(d.Ports) == 0 {
			errs = append(errs, fmt.Errorf("%s.deploy.ports: at least one port", at))
		}
		hosts := map[int32]int{}
		for j, p := range d.Ports {
			if err := p.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("%s.deploy.ports[%d]: %w", at, j, err))
			}
			if p.Forwarded() {
				if prev, dup := hosts[p.Host]; dup {
					errs = append(errs, fmt.Errorf("%s.deploy.ports[%d]: localhost:%d is also ports[%d]'s", at, j, p.Host, prev))
				}
				hosts[p.Host] = j
			}
		}
		if err := d.Healthcheck.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("%s.deploy.%w", at, err))
		}
		for j, c := range d.Init {
			if err := c.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("%s.deploy.init[%d]: %w", at, j, err))
			}
			for _, f := range c.EnvFile.ResolvePaths(decl.Dir) {
				if err := fileio.CheckLocal(f); err != nil {
					errs = append(errs, fmt.Errorf("%s.deploy.init[%d].envFile: %w", at, j, err))
				}
			}
		}
		for _, f := range d.EnvFile.ResolvePaths(decl.Dir) {
			if err := fileio.CheckLocal(f); err != nil {
				errs = append(errs, fmt.Errorf("%s.deploy.envFile: %w", at, err))
			}
		}
		if d.Replicas != nil && *d.Replicas < 0 {
			errs = append(errs, fmt.Errorf("%s.deploy.replicas cannot be negative", at))
		}
	}
	return append(errs, imageNeeds(decl)...)
}

// gitSource reports what keeps an image's git from being checked out: no
// build to run in it, a branch and a ref both, a pull policy that isn't one,
// pull on a local repository (nothing is cloned), a local path that isn't a
// git repository.
func gitSource(at string, img cluster.Image, dir string) []error {
	g := img.Git
	if g == nil {
		return nil
	}
	var errs []error
	if img.Build == nil {
		errs = append(errs, fmt.Errorf("%s.git: an image from git is built there; set build", at))
	}
	if g.URL == "" {
		return append(errs, fmt.Errorf("%s.git.url is required", at))
	}
	if g.Branch != "" && g.Ref != "" {
		errs = append(errs, fmt.Errorf("%s.git: set branch or ref, not both", at))
	}
	remote := gitsource.IsRemote(g.URL)
	switch {
	case g.Pull != "" && !remote:
		errs = append(errs, fmt.Errorf("%s.git.pull: %s is a local repository; nothing is cloned or pulled", at, g.URL))
	case g.Pull != "" && g.Pull != cluster.PullAlways && g.Pull != cluster.PullIfNotPresent:
		errs = append(errs, fmt.Errorf("%s.git.pull %q: want %s or %s", at, g.Pull, cluster.PullAlways, cluster.PullIfNotPresent))
	}
	if !remote {
		path := gitsource.LocalPath(dir, g.URL)
		if err := fileio.CheckLocal(filepath.Join(path, ".git")); err != nil {
			errs = append(errs, fmt.Errorf("%s.git.url %s: not a git repository (no .git there)", at, path))
		}
	}
	return errs
}

// imageNeeds reports what keeps the images' needs from ordering their
// deployments: two images with one name, a need that names no image or one
// that isn't deployed, an image with needs but nothing to deploy, an image
// that needs itself, a cycle.
func imageNeeds(d *spec.Declaration) []error {
	imgs := d.Environment.Images.ResolveDefaultNames(d.Name)
	byName := map[string]int{}
	var names []string
	var errs []error
	for i, img := range imgs {
		if prev, dup := byName[img.Name]; dup && img.Name != "" {
			errs = append(errs, fmt.Errorf("environment.images[%d]: name %q is also images[%d]'s", i, img.Name, prev))
			continue
		}
		byName[img.Name] = i
		names = append(names, img.Name)
	}
	needed := false
	for i, img := range imgs {
		if len(img.Needs) == 0 {
			continue
		}
		needed = true
		at := fmt.Sprintf("environment.images[%d] (%s)", i, img.Name)
		if img.Deploy == nil {
			errs = append(errs, fmt.Errorf("%s: needs orders deployments; this image has no deploy", at))
		}
		for _, n := range imgs.Needs(img) {
			j, ok := byName[n]
			switch {
			case n == img.Name:
				errs = append(errs, fmt.Errorf("%s: needs itself", at))
			case !ok:
				errs = append(errs, fmt.Errorf("%s: needs %q, which is not an image here", at, n))
			case imgs[j].Deploy == nil:
				errs = append(errs, fmt.Errorf("%s: needs %q, which is not deployed: there is nothing to be ready", at, n))
			}
		}
	}
	if !needed || len(errs) > 0 {
		return errs
	}
	if _, err := structured.OrderByNeeds(names, func(n string) []string { return imgs.Needs(imgs[byName[n]]) }); err != nil {
		errs = append(errs, fmt.Errorf("environment.images: %w", err))
	}
	return errs
}
