package runner

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/intentd/jig/pkg/gitsource"
	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/resolver/env"
	"github.com/intentd/tools/structured"
)

// deployReadyTimeout is how long a wave's Deployments have to become ready
// before the images that need them are deployed.
const deployReadyTimeout = "180s"

// loadImages builds the selected images and loads them into the kind
// cluster. Deploying them is a later phase, after the Secrets they read.
func (r *runner) loadImages(ctx context.Context) error {
	e := r.spec.Environment // nil: --images alone
	load, err := e.EffectiveLoadImages(r.spec.Name, r.o.Images)
	if err != nil || len(load) == 0 {
		return err
	}
	// A declared entry brings its build; an image only --images names is
	// loaded as it is. Build paths resolve like Compose: context against
	// the jig's directory, dockerfile against the context; an image from
	// git builds inside its checkout, and the checkouts made for this run
	// go once the images are loaded.
	declared := map[string]cluster.Image{}
	vars := r.vars()
	if e != nil {
		var imgs cluster.Images
		for _, img := range e.Images.ResolveDefaultNames(r.spec.Name) {
			if slices.Contains(load, img.Image) {
				imgs = append(imgs, img)
			}
		}
		dirs, cleanup, err := r.checkouts(ctx, vars, imgs)
		defer cleanup()
		if err != nil {
			return err
		}
		for i, img := range imgs {
			if img.Build != nil {
				b, err := expandBuild(vars, img)
				if err != nil {
					return err
				}
				b = b.ResolvePaths(or(dirs[i], r.spec.Dir))
				img.Build = &b
			}
			// The entry that builds a reference is the one that counts; one
			// that only uses it doesn't replace it.
			if prev, ok := declared[img.Image]; ok && prev.Build != nil {
				continue
			}
			declared[img.Image] = img
		}
	}
	var selected cluster.Images
	for _, ref := range load {
		img, ok := declared[ref]
		if !ok {
			img = cluster.Image{Image: ref}
		}
		selected = append(selected, img)
	}
	name, ok := r.kindClusterName(ctx)
	if !ok {
		return fmt.Errorf("loading images needs a kind cluster; context %q is not kind", r.effectiveContext(ctx))
	}
	return cluster.LoadImages(ctx, name, selected, cluster.WithBuildWorkers(r.o.Parallel))
}

// deployImages applies the Deployments and Services declared for the
// selected images, after the Secrets they read, in needs order: a wave of
// images whose needs are all deployed is applied, then waited on until
// ready, before the next. A need outside the images --images selects is
// taken as met. deploy.env is expanded through the run's env and contexts,
// so ${{ ecosystem.postgres.url }} is the plan's value.
func (r *runner) deployImages(ctx context.Context) error {
	deploy, err := r.spec.Environment.EffectiveDeployImages(r.spec.Name, r.o.Images)
	if err != nil || len(deploy) == 0 {
		return err
	}
	byName := map[string]cluster.Image{}
	names := make([]string, 0, len(deploy))
	for _, img := range deploy {
		byName[img.Name] = img
		names = append(names, img.Name)
	}
	needs := func(name string) []string {
		var in []string
		for _, n := range deploy.Needs(byName[name]) {
			if _, ok := byName[n]; ok {
				in = append(in, n)
			}
		}
		return in
	}
	waves, err := structured.WavesByNeeds(names, needs)
	if err != nil {
		return fmt.Errorf("environment.images: %w", err)
	}

	vars := r.vars()
	for i, wave := range waves {
		var imgs cluster.Images
		for _, name := range wave {
			img, err := expandDeploy(vars, envFilesFrom(byName[name], r.spec.Dir))
			if err != nil {
				return err
			}
			imgs = append(imgs, img)
		}
		if err := cluster.DeployImages(ctx, r.cs, imgs, cluster.WithFieldManager(r.o.FieldManager), cluster.WithForceOwnership()); err != nil {
			return err
		}
		// The last wave is waited on by whatever needs it: environment.wait,
		// a step, an expectation.
		if i == len(waves)-1 {
			continue
		}
		for _, img := range imgs {
			w := spec.Wait{Kind: "Deployment", Name: img.Deploy.Name, Namespace: img.Deploy.Namespace, Ready: true, Timeout: deployReadyTimeout}
			if w.Namespace == "" {
				w.Namespace = "default"
			}
			if err := runWait(ctx, w); err != nil {
				return fmt.Errorf("image %s, needed by the next images: %w", img.Name, err)
			}
		}
	}
	return nil
}

// checkoutWorkers bounds how many repositories are checked out at once.
const checkoutWorkers = 4

// checkouts checks out the repository of each image built from git, its
// git values expanded through vars first, and returns the directory of
// each (by index; "" for an image not from git) with what removes them.
// Different repositories are checked out together; the images of one
// repository one after another, since they share its clone and its
// worktree list.
func (r *runner) checkouts(ctx context.Context, vars env.Resolver, imgs cluster.Images) ([]string, func(), error) {
	dirs := make([]string, len(imgs))
	groups := map[string][]int{}
	var keys []string
	for i, img := range imgs {
		if img.Git == nil || img.Build == nil {
			continue
		}
		if err := spec.Unresolved(vars, *img.Git); err != nil {
			return dirs, func() {}, fmt.Errorf("image %s: git: %w", img.Name, err)
		}
		g := env.ExpandAll(vars, *img.Git)
		imgs[i].Git = &g
		key := gitsource.LocalPath(r.spec.Dir, g.URL)
		if gitsource.IsRemote(g.URL) {
			key = g.URL
		}
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], i)
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		cleanups []func()
		firstErr error
	)
	sem := make(chan struct{}, checkoutWorkers)
	for _, key := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			for _, i := range groups[key] {
				dir, cleanup, err := gitsource.Checkout(ctx, *imgs[i].Git, r.spec.Dir)
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("image %s: %w", imgs[i].Name, err)
					}
					mu.Unlock()
					return
				}
				dirs[i] = dir
				cleanups = append(cleanups, cleanup)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return dirs, func() {
		for _, c := range cleanups {
			c()
		}
	}, firstErr
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// expandBuild returns img's build with its values (args, context,
// dockerfile, target) expanded through vars: ${{ env.VERSION }} as a build
// arg. An unknown reference is an error naming the image.
func expandBuild(vars env.Resolver, img cluster.Image) (cluster.Build, error) {
	if err := spec.Unresolved(vars, *img.Build); err != nil {
		return cluster.Build{}, fmt.Errorf("image %s: build: %w", img.Name, err)
	}
	return env.ExpandAll(vars, *img.Build), nil
}

// envFilesFrom is img with its env files resolved from dir, the jig's
// directory; those from an include file are absolute already.
func envFilesFrom(img cluster.Image, dir string) cluster.Image {
	d := *img.Deploy
	d.EnvFile = d.EnvFile.ResolvePaths(dir)
	inits := slices.Clone(d.Init)
	for k := range inits {
		inits[k].EnvFile = inits[k].EnvFile.ResolvePaths(dir)
	}
	d.Init = inits
	img.Deploy = &d
	return img
}

// expandDeploy returns img with its deploy.env and envFrom expanded through
// vars; an unknown reference is an error naming the image.
func expandDeploy(vars env.Resolver, img cluster.Image) (cluster.Image, error) {
	d := *img.Deploy
	values := struct {
		Env     map[string]string
		EnvFrom []string
		Init    []cluster.InitContainer
	}{d.Env, d.EnvFrom, d.Init}
	if err := spec.Unresolved(vars, values); err != nil {
		return img, fmt.Errorf("image %s: deploy: %w", img.Name, err)
	}
	values = env.ExpandAll(vars, values)
	d.Env, d.EnvFrom, d.Init = values.Env, slices.Clone(values.EnvFrom), values.Init
	img.Deploy = &d
	return img, nil
}
