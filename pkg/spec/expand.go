package spec

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/intentd/jig/pkg/gitsource"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/resolver"
)

// Includes on the types: wherever images, apply entries or Helm releases are
// listed, an entry can be include: <file>, and the file's entries replace it.
// The file holds them under the type's own key (images:, apply:, helm:), so
// one file serves every place the type appears; the paths in it resolve from
// the file. An include file can include others; a file that includes itself
// is an error naming the chain. An entry with include sets nothing else.

// ImagesInclude is the file an include: entry in an images list names.
type ImagesInclude struct {
	Images cluster.Images `yaml:"images"`
}

// ApplyInclude is the file an include: entry in an apply list names.
type ApplyInclude struct {
	Apply ApplyEntries `yaml:"apply"`
}

// HelmInclude is the file an include: entry in a helm list, or a step's
// helm, names.
type HelmInclude struct {
	Helm HelmInstalls `yaml:"helm"`
}

// ExpandIncludes expands the includes of the environment's images, apply
// and helm lists, relative paths resolved against baseDir.
func (e *Environment) ExpandIncludes(baseDir string) error {
	if e == nil {
		return nil
	}
	images, err := expandImages(e.Images, baseDir, nil)
	if err != nil {
		return fmt.Errorf("environment.images: %w", err)
	}
	e.Images = images
	if err := e.Provision.ExpandIncludes(baseDir); err != nil {
		return fmt.Errorf("environment.%w", err)
	}
	return nil
}

// ExpandIncludes expands the includes of p's apply and helm lists, relative
// paths resolved against baseDir.
func (p *Provision) ExpandIncludes(baseDir string) error {
	apply, err := p.Apply.ExpandIncludes(baseDir)
	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	helm, err := p.Helm.ExpandIncludes(baseDir)
	if err != nil {
		return fmt.Errorf("helm: %w", err)
	}
	p.Apply, p.Helm = apply, helm
	return nil
}

// ExpandIncludes expands the includes of each step's apply and helm. A
// step's helm is one release: its include file must hold exactly one.
func (s Steps) ExpandIncludes(baseDir string) error {
	for i := range s {
		st := &s[i]
		apply, err := st.Apply.ExpandIncludes(baseDir)
		if err != nil {
			return fmt.Errorf("prepare %q: apply: %w", st.Name, err)
		}
		st.Apply = apply
		if st.Helm == nil || st.Helm.Include == "" {
			continue
		}
		helm, err := HelmInstalls{*st.Helm}.ExpandIncludes(baseDir)
		if err != nil {
			return fmt.Errorf("prepare %q: helm: %w", st.Name, err)
		}
		if len(helm) != 1 {
			return fmt.Errorf("prepare %q: helm: include %s: a step installs one release; the file has %d",
				st.Name, st.Helm.Include, len(helm))
		}
		st.Helm = &helm[0]
	}
	return nil
}

// ExpandIncludes replaces each include: entry with the entries its file
// lists, their paths resolved from that file.
func (a ApplyEntries) ExpandIncludes(baseDir string) (ApplyEntries, error) {
	return expandEntries(a, baseDir, nil, includeKind[ApplyEntry]{
		include: func(e ApplyEntry) string { return e.Include },
		alone:   func(e ApplyEntry) bool { return reflect.DeepEqual(e, ApplyEntry{Include: e.Include}) },
		read: func(data []byte) ([]ApplyEntry, error) {
			var f ApplyInclude
			err := strictUnmarshal(data, &f)
			return f.Apply, err
		},
		resolve: func(e ApplyEntry, dir string) ApplyEntry {
			e.Path = resolvePath(dir, e.Path)
			return e
		},
	})
}

// ExpandIncludes replaces each include: entry with the releases its file
// lists, their local chart and values paths resolved from that file.
func (h HelmInstalls) ExpandIncludes(baseDir string) (HelmInstalls, error) {
	return expandEntries(h, baseDir, nil, includeKind[HelmInstall]{
		include: func(r HelmInstall) string { return r.Include },
		alone:   func(r HelmInstall) bool { return reflect.DeepEqual(r, HelmInstall{Include: r.Include}) },
		read: func(data []byte) ([]HelmInstall, error) {
			var f HelmInclude
			err := strictUnmarshal(data, &f)
			return f.Helm, err
		},
		resolve: func(r HelmInstall, dir string) HelmInstall {
			if r.IsLocalChart() {
				r.Chart = resolvePath(dir, r.Chart)
			}
			files := make([]string, len(r.ValueFiles))
			for i, v := range r.ValueFiles {
				if !strings.Contains(v, "://") {
					v = resolvePath(dir, v)
				}
				files[i] = v
			}
			if len(files) > 0 {
				r.ValueFiles = files
			}
			return r
		},
	})
}

// expandImages replaces each include: entry with the images its file lists,
// their build paths resolved from that file.
func expandImages(imgs cluster.Images, baseDir string, chain []string) (cluster.Images, error) {
	return expandEntries(imgs, baseDir, chain, includeKind[cluster.Image]{
		include: func(i cluster.Image) string { return i.Include },
		alone:   func(i cluster.Image) bool { return reflect.DeepEqual(i, cluster.Image{Include: i.Include}) },
		read: func(data []byte) ([]cluster.Image, error) {
			var f ImagesInclude
			err := strictUnmarshal(data, &f)
			return f.Images, err
		},
		resolve: func(i cluster.Image, dir string) cluster.Image {
			// A forEach entry's rows are decoded later: they resolve from
			// this file then.
			if i.ForEach != nil {
				if i.Dir == "" {
					i.Dir = dir
				}
				return i
			}
			return resolveImage(i, dir)
		},
	})
}

// resolveImage is i with its relative paths resolved from dir, the
// directory of the include file that holds it. An image from git builds
// inside its checkout: a local repository's path resolves from the file,
// the build paths stay relative to the checkout.
func resolveImage(i cluster.Image, dir string) cluster.Image {
	i = resolveEnvFiles(i, dir)
	if g := i.Git; g != nil {
		if !gitsource.IsRemote(g.URL) {
			c := *g
			c.URL = gitsource.LocalPath(dir, g.URL)
			i.Git = &c
		}
		return i
	}
	if i.Build != nil {
		b := i.Build.ResolvePaths(dir)
		i.Build = &b
	}
	return i
}

// resolveEnvFiles is i with its deploy's env files (and its inits')
// resolved from dir, the file that declares them.
func resolveEnvFiles(i cluster.Image, dir string) cluster.Image {
	if i.Deploy == nil {
		return i
	}
	d := *i.Deploy
	d.EnvFile = d.EnvFile.ResolvePaths(dir)
	if len(d.Init) > 0 {
		inits := slices.Clone(d.Init)
		for k := range inits {
			inits[k].EnvFile = inits[k].EnvFile.ResolvePaths(dir)
		}
		d.Init = inits
	}
	i.Deploy = &d
	return i
}

// ExpandForEach repeats each image with forEach once per row (the templates
// read .item, with KEL's functions and fns), after the includes: every
// image the run sees is concrete. A row from an include file resolves its
// paths from that file, as the entry would have.
func (e *Environment) ExpandForEach(fns []resolver.Function) error {
	if e == nil {
		return nil
	}
	rows, err := e.Images.ExpandForEach(resolver.NewWithFunctions(nil, fns...))
	if err != nil {
		return fmt.Errorf("environment.%w", err)
	}
	for i, img := range rows {
		if img.Row != nil && img.Dir != "" {
			rows[i] = resolveImage(img, img.Dir)
		}
	}
	e.Images = rows
	return nil
}

// includeKind is what expandEntries needs to know about one type.
type includeKind[T any] struct {
	include func(T) string            // the entry's include, or ""
	alone   func(T) bool              // whether the entry sets nothing beside include
	read    func([]byte) ([]T, error) // the file's entries, strictly
	resolve func(T, string) T         // the entry with its relative paths resolved from a directory
}

// expandEntries expands the include: entries of items, resolved against dir.
// chain is the include files being expanded, outermost first.
func expandEntries[S ~[]T, T any](items S, dir string, chain []string, k includeKind[T]) (S, error) {
	if !slices.ContainsFunc(items, func(t T) bool { return k.include(t) != "" }) {
		return items, nil
	}
	var out S
	for _, item := range items {
		inc := k.include(item)
		if inc == "" {
			out = append(out, item)
			continue
		}
		if !k.alone(item) {
			return nil, fmt.Errorf("include %s: an entry with include sets nothing else", inc)
		}
		path := inc
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", inc, err)
		}
		if slices.Contains(chain, abs) {
			return nil, fmt.Errorf("include %s: includes itself: %s", inc, strings.Join(append(slices.Clone(chain), abs), " → "))
		}
		data, err := readLocal(abs)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", inc, err)
		}
		entries, err := k.read(data)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", inc, err)
		}
		includeDir := filepath.Dir(abs)
		nested, err := expandEntries(S(entries), includeDir, append(slices.Clone(chain), abs), k)
		if err != nil {
			return nil, fmt.Errorf("include %s: %w", inc, err)
		}
		// Entries from deeper files are resolved already: their paths are
		// absolute, and resolving again leaves them as they are.
		for _, e := range nested {
			out = append(out, k.resolve(e, includeDir))
		}
	}
	return out, nil
}

// resolvePath returns p resolved from dir, or p when it is absolute or empty.
func resolvePath(dir, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}
