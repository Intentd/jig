// Package post tears a jig run down: what it applied and installed, removed
// in reverse once its expectations and imports have run.
package post

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/tools/cluster"
	"gopkg.in/yaml.v3"
	"k8s.io/utils/ptr"
)

// teardownTimeout bounds every delete in teardown. A delete that outlasts it
// is almost always a finalizer whose controller is already gone.
const teardownTimeout = "60s"

// Undo is one thing a run did, to undo at teardown: a manifest it applied
// (Path) or a Helm release it installed (Release).
type Undo struct {
	// Label names it in the output, such as "prepare order.yaml".
	Label   string
	Path    string
	Release *cluster.HelmRelease
	// Keep leaves it in place: a release a parent run owns, which it removes
	// after its last import.
	Keep bool
	// File is a generated manifest to remove from disk once deleted.
	File bool
	// Do undoes it in code, for what is neither a manifest nor a release:
	// a Secret the run wrote, a namespace it created.
	Do func(context.Context) error
}
type Undos []Undo

// Teardown undoes what a run did. Custom resources in every applied
// manifest go first, newest first, while the operators that finalize them
// still run; then each Undo in reverse: manifests deleted, releases
// uninstalled.
func Teardown(ctx context.Context, undos Undos) {
	if len(undos) == 0 {
		return
	}
	fmt.Printf("\n→ Cleaning up resources...\n")

	for i := len(undos) - 1; i >= 0; i-- {
		if u := undos[i]; u.Path != "" && !u.Keep {
			deleteCustomResources(ctx, u.Path)
		}
	}
	for i := len(undos) - 1; i >= 0; i-- {
		u := undos[i]
		switch {
		case u.Keep:
		case u.Release != nil:
			sp := startSpinner(fmt.Sprintf("Uninstalling %s...", u.Label))
			if err := cluster.HelmUninstall(ctx, *u.Release); err != nil {
				sp.Failure()
				fmt.Printf("    %v\n", err)
			} else {
				sp.Success()
			}
		case u.Do != nil:
			sp := startSpinner(fmt.Sprintf("Deleting %s...", u.Label))
			if err := u.Do(ctx); err != nil {
				sp.Failure()
				fmt.Printf("    %v\n", err)
			} else {
				sp.Success()
			}
		case u.Path != "":
			deleteStep(ctx, fmt.Sprintf("Deleting %s...", u.Label), u.Path)
		}
		if u.File && u.Path != "" {
			os.Remove(u.Path)
		}
	}
	fmt.Printf("  %s Cleanup complete\n", successMark())
}

func deleteStep(ctx context.Context, msg, path string) {
	sp := startSpinner(msg)
	onTimeout := func() { sp.Update(strings.TrimSuffix(msg, "...") + " (finalizers cleared)") }
	if err := deleteManifest(ctx, path, onTimeout); err != nil {
		sp.Failure()
		fmt.Printf("    %v\n", err)
		return
	}
	sp.Success()
}

// deleteCustomResources deletes the custom resources in path and leaves
// everything else for the later teardown steps.
func deleteCustomResources(ctx context.Context, path string) {
	docs, err := manifestDocs(path)
	if err != nil {
		return
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	n := 0
	for _, d := range docs {
		if isCustomGroup(d.apiVersion) {
			if err := enc.Encode(d.node); err != nil {
				return
			}
			n++
		}
	}
	if err := enc.Close(); err != nil || n == 0 {
		return
	}

	tmp, err := os.CreateTemp("", "inrun-e2e-crs-*.yaml")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return
	}
	tmp.Close()

	deleteStep(ctx, fmt.Sprintf("Deleting custom resources from %s...", filepath.Base(path)), tmp.Name())
}

// deleteManifest deletes everything in path. If the delete times out, it
// calls onTimeout, clears the finalizers on what is left, including custom
// resources of any CRD in path, and deletes again without waiting.
func deleteManifest(ctx context.Context, path string, onTimeout func()) error {
	out, err := kubectl.Delete(ctx, kubectl.DeleteOptions{
		File:           path,
		IgnoreNotFound: true,
		Timeout:        teardownTimeout,
	})
	if err == nil {
		return nil
	}
	if !strings.Contains(out, "timed out") {
		return fmt.Errorf("%w\n%s", err, out)
	}

	onTimeout()
	_, _ = kubectl.PatchFile(ctx, kubectl.PatchFileOptions{
		File:  path,
		Type:  "merge",
		Patch: `{"metadata":{"finalizers":null}}`,
	})
	if docs, err := manifestDocs(path); err == nil {
		for _, d := range docs {
			if d.kind == "CustomResourceDefinition" && d.name != "" {
				clearCustomResourceFinalizers(ctx, d.name)
			}
		}
	}

	if out, err := kubectl.Delete(ctx, kubectl.DeleteOptions{
		File:           path,
		IgnoreNotFound: true,
		Wait:           ptr.To(false),
	}); err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

// clearCustomResourceFinalizers clears the finalizers on every resource of
// crd (plural.group), so a CRD stuck deleting can finish.
func clearCustomResourceFinalizers(ctx context.Context, crd string) {
	out, err := kubectl.Get(ctx, kubectl.GetOptions{
		Kind:          crd,
		AllNamespaces: true,
		Format:        `jsonpath={range .items[*]}{.metadata.namespace}{"/"}{.metadata.name}{"\n"}{end}`,
	})
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		ns, name, ok := strings.Cut(strings.TrimSpace(line), "/")
		if !ok || name == "" {
			continue
		}
		_, _ = kubectl.Patch(ctx, kubectl.PatchOptions{
			Kind:      crd,
			Name:      name,
			Namespace: ns,
			Type:      "merge",
			Patch:     `{"metadata":{"finalizers":null}}`,
		})
	}
}

// manifestDoc is one YAML document of a manifest file.
type manifestDoc struct {
	node       *yaml.Node
	apiVersion string
	kind       string
	name       string
}

// manifestDocs splits the manifest at path into its documents.
func manifestDocs(path string) ([]manifestDoc, error) {
	data, err := readLocal(path)
	if err != nil {
		return nil, err
	}
	var docs []manifestDoc
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var node yaml.Node
		if err := dec.Decode(&node); err != nil {
			if errors.Is(err, io.EOF) {
				return docs, nil
			}
			return nil, err
		}
		var meta struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
			Metadata   struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}
		if err := node.Decode(&meta); err != nil || meta.Kind == "" {
			continue
		}
		docs = append(docs, manifestDoc{node: &node, apiVersion: meta.APIVersion, kind: meta.Kind, name: meta.Metadata.Name})
	}
}

// isCustomGroup reports whether apiVersion belongs to a custom resource.
// Built-in groups are either undotted (apps, batch) or end in .k8s.io.
func isCustomGroup(apiVersion string) bool {
	group, _, ok := strings.Cut(apiVersion, "/")
	return ok && strings.Contains(group, ".") && !strings.HasSuffix(group, ".k8s.io")
}
