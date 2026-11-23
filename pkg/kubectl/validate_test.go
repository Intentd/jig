package kubectl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/assert"
)

// ── Validate ─────────────────────────────────────────────────────────────────

func TestValidate_NoKubectlBlock(t *testing.T) {
	expects := []spec.Expectation{{Name: "plain"}}
	errs := Validate(expects, "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidate_WrapsWithLocation(t *testing.T) {
	expects := []spec.Expectation{{
		Name:    "step",
		Kubectl: &spec.Kubectl{Get: []spec.KubectlGet{{}}},
	}}
	errs := Validate(expects, "")
	if len(errs) == 0 {
		t.Fatal("expected errors")
	}
	if !strings.Contains(errs[0].Error(), `expect[0] "step" kubectl.get[0]`) {
		t.Errorf("location missing from %q", errs[0])
	}
}

// get ─────────────────────────────────────────────────────────────────────────

func TestValidateKubectl_GetMissingKind(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Name:   "foo",
		Field:  ".status",
		Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "ok"}},
	})
	requireErr(t, errs, "kind")
}

func TestValidateKubectl_GetMissingNameAndSelector(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:   "Deployment",
		Field:  ".status",
		Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "ok"}},
	})
	requireErr(t, errs, "name or labelSelector")
}

func TestValidateKubectl_GetNameAndSelectorMutuallyExclusive(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:          "Deployment",
		Name:          "app",
		LabelSelector: "app=foo",
		Assert:        &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "ok"}},
	})
	requireErr(t, errs, "mutually exclusive")
}

func TestValidateKubectl_GetBySelector(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:          "Deployment",
		LabelSelector: "app=foo",
		Assert:        &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "ok"}},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateKubectl_GetNoAssertion(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:  "Deployment",
		Name:  "app",
		Field: ".status",
	})
	requireErr(t, errs, "assertion")
}

func TestValidateKubectl_GetInvalidFormat(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:   "Deployment",
		Name:   "app",
		Format: "xml",
		Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "ok"}},
	})
	requireErr(t, errs, "json or yaml")
}

func TestValidateKubectl_GetJQWithoutJSON(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:   "Deployment",
		Name:   "app",
		Format: "yaml",
		JQ:     ".metadata",
		Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "ok"}},
	})
	requireErr(t, errs, "jq requires format: json")
}

func TestValidateKubectl_GetYQWithoutYAML(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:   "Deployment",
		Name:   "app",
		Format: "json",
		YQ:     ".metadata",
		Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "ok"}},
	})
	requireErr(t, errs, "yq requires format: yaml")
}

func TestValidateKubectl_GetJSONPathWithoutAssertion(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:     "Deployment",
		Name:     "app",
		JSONPath: "{.status.phase}",
	})
	requireErr(t, errs, "jsonPath requires an assertion")
}

func TestValidateKubectl_GetJSONPathWithAssertion(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:     "Deployment",
		Name:     "app",
		JSONPath: "{.status.phase}",
		Assert:   &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "Running"}},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateKubectl_GetValid(t *testing.T) {
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:   "Deployment",
		Name:   "app",
		Field:  ".status.phase",
		Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "Running"}},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateKubectl_GetExistsFalseWithoutExtractor(t *testing.T) {
	f := false
	errs := validateKubectlGet("loc", spec.KubectlGet{
		Kind:   "Deployment",
		Name:   "app",
		Assert: &assert.Assertions{Exists: &f},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// logs ────────────────────────────────────────────────────────────────────────

func TestValidateKubectl_LogsMissingSelector(t *testing.T) {
	errs := validateKubectlLogs("loc", spec.KubectlLogs{
		Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "started"}},
	})
	requireErr(t, errs, "name, labelSelector, or leaderElection")
}

func TestValidateKubectl_LogsNameAndSelectorMutuallyExclusive(t *testing.T) {
	errs := validateKubectlLogs("loc", spec.KubectlLogs{
		Name:          "my-pod",
		LabelSelector: "app=foo",
		Assert:        &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "started"}},
	})
	requireErr(t, errs, "mutually exclusive")
}

func TestValidateKubectl_LogsNoAssertion(t *testing.T) {
	errs := validateKubectlLogs("loc", spec.KubectlLogs{Name: "my-pod"})
	requireErr(t, errs, "assertion")
}

func TestValidateKubectl_LogsLeaderElectionMissingLease(t *testing.T) {
	errs := validateKubectlLogs("loc", spec.KubectlLogs{
		LeaderElection: &spec.KubectlLeaderElection{},
		Assert:         &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "started"}},
	})
	requireErr(t, errs, "leaderElection.lease")
}

func TestValidateKubectl_LogsLeaderElectionMutuallyExclusiveWithName(t *testing.T) {
	errs := validateKubectlLogs("loc", spec.KubectlLogs{
		Name:           "my-pod",
		LeaderElection: &spec.KubectlLeaderElection{Lease: "my-lease"},
		Assert:         &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "started"}},
	})
	requireErr(t, errs, "mutually exclusive")
}

func TestValidateKubectl_LogsLeaderElectionValid(t *testing.T) {
	errs := validateKubectlLogs("loc", spec.KubectlLogs{
		LeaderElection: &spec.KubectlLeaderElection{Lease: "my-lease"},
		Assert:         &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "started"}},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// describe ────────────────────────────────────────────────────────────────────

func TestValidateKubectl_DescribeNameAndSelectorMutuallyExclusive(t *testing.T) {
	errs := validateKubectlDescribe("loc", spec.KubectlDescribe{
		Kind:          "Pod",
		Name:          "x",
		LabelSelector: "app=foo",
		Assert:        &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "Running"}},
	})
	requireErr(t, errs, "mutually exclusive")
}

// exec ────────────────────────────────────────────────────────────────────────

func TestValidateKubectl_ExecNameAndSelectorMutuallyExclusive(t *testing.T) {
	errs := validateKubectlExec("loc", spec.KubectlExec{
		Name:          "my-pod",
		LabelSelector: "app=foo",
		Command:       []string{"cat", "/x"},
		Assert:        &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "x"}},
	})
	requireErr(t, errs, "mutually exclusive")
}

// port-forward ────────────────────────────────────────────────────────────────

// Without a path, a check that asserts requests "/"; one that asserts
// nothing checks only the forward.
func TestValidateKubectl_PortForwardAssertionWithoutPath(t *testing.T) {
	pf := spec.KubectlPortForward{
		Service: "my-svc",
		Port:    8080,
		Assert:  &assert.Assertions{TextAssertion: assert.TextAssertion{Equals: "ok"}},
	}
	if errs := validateKubectlPortForward("loc", pf, ""); len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if got := pf.RequestPath(); got != "/" {
		t.Fatalf("RequestPath() = %q, want /", got)
	}
	if got := (spec.KubectlPortForward{Service: "db", Port: 5432}).RequestPath(); got != "" {
		t.Fatalf("RequestPath() = %q for a forward-only check, want none", got)
	}
}

func TestValidateKubectl_PortForwardNoServiceOrPod(t *testing.T) {
	errs := validateKubectlPortForward("loc", spec.KubectlPortForward{Port: 8080}, "")
	requireErr(t, errs, "service, pod, or leaderElection")
}

func TestValidateKubectl_PortForwardServiceAndPodMutuallyExclusive(t *testing.T) {
	errs := validateKubectlPortForward("loc", spec.KubectlPortForward{
		Service: "my-svc",
		Pod:     "my-pod",
		Port:    8080,
	}, "")
	requireErr(t, errs, "mutually exclusive")
}

func TestValidateKubectl_PortForwardZeroPort(t *testing.T) {
	errs := validateKubectlPortForward("loc", spec.KubectlPortForward{
		Service: "my-svc",
		Port:    0,
	}, "")
	requireErr(t, errs, "port must be > 0")
}

func TestValidateKubectl_PortForwardBodyWithoutPath(t *testing.T) {
	pf := spec.KubectlPortForward{
		Service: "my-svc",
		Port:    8080,
		Method:  "POST",
		Body:    `{"x":1}`,
	}
	if errs := validateKubectlPortForward("loc", pf, ""); len(errs) != 0 {
		t.Fatalf("errs = %v, want none", errs)
	}
	if got := pf.RequestPath(); got != "/" {
		t.Fatalf("RequestPath() = %q, want /", got)
	}
}

func TestValidateKubectl_PortForwardBodyAndBodyFileMutuallyExclusive(t *testing.T) {
	errs := validateKubectlPortForward("loc", spec.KubectlPortForward{
		Service:  "my-svc",
		Port:     8080,
		Path:     "/x",
		Method:   "POST",
		Body:     `{"x":1}`,
		BodyFile: "body.json",
	}, "")
	requireErr(t, errs, "mutually exclusive")
}

func TestValidateKubectl_PortForwardGetWithBody(t *testing.T) {
	errs := validateKubectlPortForward("loc", spec.KubectlPortForward{
		Service: "my-svc",
		Port:    8080,
		Path:    "/x",
		Method:  "GET",
		Body:    `{"x":1}`,
	}, "")
	requireErr(t, errs, "does not accept a body")
}

func TestValidateKubectl_PortForwardBodyFileMissing(t *testing.T) {
	dir := t.TempDir()
	errs := validateKubectlPortForward("loc", spec.KubectlPortForward{
		Service:  "my-svc",
		Port:     8080,
		Path:     "/x",
		Method:   "POST",
		BodyFile: "missing.json",
	}, dir)
	requireErr(t, errs, "bodyFile")
}

func TestValidateKubectl_PortForwardBodyFileExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "body.json"), []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	errs := validateKubectlPortForward("loc", spec.KubectlPortForward{
		Service:  "my-svc",
		Port:     8080,
		Path:     "/x",
		Method:   "POST",
		BodyFile: "body.json",
	}, dir)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateKubectl_PortForwardValidNoAssertion(t *testing.T) {
	errs := validateKubectlPortForward("loc", spec.KubectlPortForward{
		Service: "my-svc",
		Port:    8080,
	}, "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidateKubectl_PortForwardLeaderElectionMissingLease(t *testing.T) {
	errs := validateKubectlPortForward("loc", spec.KubectlPortForward{
		Port:           8080,
		LeaderElection: &spec.KubectlLeaderElection{},
	}, "")
	requireErr(t, errs, "leaderElection.lease")
}

// delete ─────────────────────────────────────────────────────────────────────

func TestValidateKubectl_DeleteNeitherFileNorKind(t *testing.T) {
	errs := validateKubectlDelete("loc", spec.KubectlDelete{}, "")
	requireErr(t, errs, "file or (kind + name)")
}

func TestValidateKubectl_DeleteMutuallyExclusive(t *testing.T) {
	errs := validateKubectlDelete("loc", spec.KubectlDelete{
		File: "./foo.yaml",
		Kind: "Deployment",
		Name: "app",
	}, "")
	requireErr(t, errs, "mutually exclusive")
}

func TestValidateKubectl_DeleteTimeoutInvalid(t *testing.T) {
	errs := validateKubectlDelete("loc", spec.KubectlDelete{
		File:    "./foo.yaml",
		Timeout: "not-a-duration",
	}, "")
	requireErr(t, errs, "valid duration")
}

func TestValidateKubectl_DeleteByFile(t *testing.T) {
	errs := validateKubectlDelete("loc", spec.KubectlDelete{File: "./foo.yaml"}, "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// apply ──────────────────────────────────────────────────────────────────────

func TestValidateKubectl_ApplyNeitherFileNorInline(t *testing.T) {
	errs := validateKubectlApply("loc", spec.KubectlApply{}, "")
	requireErr(t, errs, "file or inline")
}

func TestValidateKubectl_ApplyMutuallyExclusive(t *testing.T) {
	errs := validateKubectlApply("loc", spec.KubectlApply{
		File:   "./foo.yaml",
		Inline: "apiVersion: v1",
	}, "")
	requireErr(t, errs, "mutually exclusive")
}

// A file an apply or delete names is checked from the step's directory; a
// URL or a templated path is left to the run.
func TestValidateKubectl_ApplyAndDeleteFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cr.yaml"), []byte("kind: X\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	requireErr(t, validateKubectlApply("loc", spec.KubectlApply{File: "./missing.yaml"}, dir), `file "./missing.yaml"`)
	requireErr(t, validateKubectlDelete("loc", spec.KubectlDelete{File: "../missing.yaml"}, dir), `file "../missing.yaml"`)

	for _, file := range []string{
		"./cr.yaml",
		filepath.Join(dir, "cr.yaml"),
		"https://example.com/cr.yaml",
		"./{{ .env.NAME }}.yaml",
		"./${NAME}.yaml",
	} {
		if errs := validateKubectlApply("loc", spec.KubectlApply{File: file}, dir); len(errs) != 0 {
			t.Errorf("apply %q: unexpected errors: %v", file, errs)
		}
		if errs := validateKubectlDelete("loc", spec.KubectlDelete{File: file}, dir); len(errs) != 0 {
			t.Errorf("delete %q: unexpected errors: %v", file, errs)
		}
	}
}

// patch ──────────────────────────────────────────────────────────────────────

func TestValidateKubectl_PatchInvalidType(t *testing.T) {
	errs := validateKubectlPatch("loc", spec.KubectlPatch{
		Kind:  "Deployment",
		Name:  "app",
		Patch: `{"spec":{}}`,
		Type:  "invalid",
	}, "")
	requireErr(t, errs, "merge, strategic, or json")
}

// A patch targets kind and name, or every object in a manifest by file: one
// or the other. With file, the manifest names each namespace.
func TestValidateKubectl_PatchFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cr.yaml"), []byte("kind: X\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := `{"spec":{}}`

	if errs := validateKubectlPatch("loc", spec.KubectlPatch{File: "./cr.yaml", Patch: patch}, dir); len(errs) != 0 {
		t.Errorf("file only: unexpected errors: %v", errs)
	}
	requireErr(t, validateKubectlPatch("loc", spec.KubectlPatch{File: "./cr.yaml", Kind: "X", Name: "a", Patch: patch}, dir), "mutually exclusive")
	requireErr(t, validateKubectlPatch("loc", spec.KubectlPatch{File: "./cr.yaml", Namespace: "team", Patch: patch}, dir), "namespace has no effect")
	requireErr(t, validateKubectlPatch("loc", spec.KubectlPatch{File: "./missing.yaml", Patch: patch}, dir), `file "./missing.yaml"`)
	requireErr(t, validateKubectlPatch("loc", spec.KubectlPatch{Patch: patch}, dir), "kind is required (or file)")
}

// The patch content comes inline or from patchFile, one or the other. One
// file as both target and patch must hold exactly one object: kubectl patches
// every object the file names with its first.
func TestValidateKubectl_PatchFileContent(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("one.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\ndata: {b: \"2\"}\n")
	write("two.yaml", "kind: ConfigMap\nmetadata: {name: a}\n---\nkind: ConfigMap\nmetadata: {name: b}\n")
	write("patch.yaml", "data: {b: \"2\"}\n")

	ok := []spec.KubectlPatch{
		{File: "./one.yaml", PatchFile: "./one.yaml"},
		{File: "./two.yaml", PatchFile: "./patch.yaml"},
		{Kind: "ConfigMap", Name: "a", PatchFile: "./patch.yaml"},
	}
	for _, p := range ok {
		if errs := validateKubectlPatch("loc", p, dir); len(errs) != 0 {
			t.Errorf("%+v: unexpected errors: %v", p, errs)
		}
	}
	requireErr(t, validateKubectlPatch("loc", spec.KubectlPatch{File: "./two.yaml", PatchFile: "./two.yaml"}, dir), "must hold one object; it holds 2")
	requireErr(t, validateKubectlPatch("loc", spec.KubectlPatch{Kind: "ConfigMap", Name: "a", Patch: "{}", PatchFile: "./patch.yaml"}, dir), "mutually exclusive")
	requireErr(t, validateKubectlPatch("loc", spec.KubectlPatch{Kind: "ConfigMap", Name: "a"}, dir), "patch or patchFile is required")
	requireErr(t, validateKubectlPatch("loc", spec.KubectlPatch{Kind: "ConfigMap", Name: "a", PatchFile: "./missing.yaml"}, dir), `patchFile "./missing.yaml"`)
}

func TestValidateKubectl_PatchValid(t *testing.T) {
	errs := validateKubectlPatch("loc", spec.KubectlPatch{
		Kind:  "Deployment",
		Name:  "app",
		Patch: `{"spec":{}}`,
	}, "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// cp ─────────────────────────────────────────────────────────────────────────

func TestValidateKubectl_CpNameAndSelectorMutuallyExclusive(t *testing.T) {
	errs := validateKubectlCp("loc", spec.KubectlCp{
		Name:          "my-pod",
		LabelSelector: "app=foo",
		Src:           "/x",
		Assert:        &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "x"}},
	})
	requireErr(t, errs, "mutually exclusive")
}

// top ────────────────────────────────────────────────────────────────────────

func TestValidateKubectl_TopInvalidKind(t *testing.T) {
	errs := validateKubectlTop("loc", spec.KubectlTop{
		Kind:   "deployment",
		Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "cpu"}},
	})
	requireErr(t, errs, "pod or node")
}

func TestValidateKubectl_TopNameAndSelectorMutuallyExclusive(t *testing.T) {
	errs := validateKubectlTop("loc", spec.KubectlTop{
		Kind:          "pod",
		Name:          "x",
		LabelSelector: "app=foo",
		Assert:        &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "cpu"}},
	})
	requireErr(t, errs, "mutually exclusive")
}

func TestValidateKubectl_TopValidPod(t *testing.T) {
	errs := validateKubectlTop("loc", spec.KubectlTop{
		Kind:   "pod",
		Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "cpu"}},
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// scale ──────────────────────────────────────────────────────────────────────

func TestValidateKubectl_ScaleNegativeReplicas(t *testing.T) {
	errs := validateKubectlScale("loc", spec.KubectlScale{
		Kind:     "Deployment",
		Name:     "app",
		Replicas: -1,
	})
	requireErr(t, errs, "replicas must be >= 0")
}

func TestValidateKubectl_ScaleValid(t *testing.T) {
	errs := validateKubectlScale("loc", spec.KubectlScale{
		Kind:     "Deployment",
		Name:     "app",
		Replicas: 3,
	})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// ── post blocks ────────────────────────────────────────
