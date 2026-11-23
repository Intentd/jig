package kubectl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/fileio"
	"github.com/intentd/tools/resolver/env"
	"github.com/intentd/tools/structured"
)

// checkSnapshot compares e's object, normalized, with its snapshot file.
// A missing file is written (the first run); drift fails with the lines
// that differ, or, with update, rewrites the file. It returns a note for
// the output when it wrote the file.
func checkSnapshot(ctx context.Context, e spec.KubectlGet, vars env.Resolver, workDir string) (string, error) {
	out, err := Get(ctx, GetOptions{Kind: e.Kind, Name: e.Name, Namespace: e.Namespace, Format: "json", WorkDir: workDir, Env: vars})
	if err != nil {
		return "", fmt.Errorf("snapshot: %w\n%s", err, out)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return "", fmt.Errorf("snapshot: %w", err)
	}
	got, err := yaml.Marshal(normalize(obj))
	if err != nil {
		return "", err
	}
	path := e.Snapshot.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	write := func(note string) (string, error) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		// Written as other generated YAML is: formatted, 2-space indent.
		if err := structured.WriteFileAndFormat(path, got, 0o644); err != nil {
			return "", err
		}
		return note, nil
	}
	want, err := fileio.ReadLocal(path)
	if err != nil {
		return write(fmt.Sprintf("snapshot %s written (first run)", e.Snapshot.File))
	}
	if string(want) == string(got) {
		return "", nil
	}
	diff, err := snapshotDiff(want, got)
	if err != nil {
		return "", fmt.Errorf("snapshot %s: %w", e.Snapshot.File, err)
	}
	if diff == "" {
		return "", nil // the same object, written differently
	}
	if e.Snapshot.Update {
		return write(fmt.Sprintf("snapshot %s updated (update: true):\n%s", e.Snapshot.File, diff))
	}
	return "", fmt.Errorf("snapshot %s differs (update: true rewrites it):\n%s", e.Snapshot.File, diff)
}

// normalize is obj without what changes from one run to the next: status,
// identity, timestamps, managed fields and the last-applied annotation.
func normalize(obj map[string]any) map[string]any {
	out := structured.DeepCopyMap(obj)
	delete(out, "status")
	if meta, ok := out["metadata"].(map[string]any); ok {
		for _, k := range []string{"uid", "resourceVersion", "generation", "creationTimestamp", "managedFields", "selfLink"} {
			delete(meta, k)
		}
		if ann, ok := meta["annotations"].(map[string]any); ok {
			delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
			delete(ann, "deployment.kubernetes.io/revision")
			if len(ann) == 0 {
				delete(meta, "annotations")
			}
		}
	}
	return out
}

// snapshotDiff is what differs between the snapshot and the object now,
// one path per line (tools' structured.Diff); "" when they are the same
// object.
func snapshotDiff(want, got []byte) (string, error) {
	var w, g any
	if err := yaml.Unmarshal(want, &w); err != nil {
		return "", fmt.Errorf("reading the file: %w", err)
	}
	if err := yaml.Unmarshal(got, &g); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, d := range structured.Diff(w, g) {
		fmt.Fprintf(&b, "  %s\n", d)
	}
	return b.String(), nil
}
