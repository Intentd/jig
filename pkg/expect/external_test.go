package expect

import (
	"context"
	"github.com/intentd/jig/pkg/spec"
	check "github.com/intentd/tools/assert"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/resolver"
	"k8s.io/client-go/kubernetes"
)

func healthServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/down" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"state":"healthy"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func call(name, url string) spec.ExternalCheck {
	return Call(name, url)
}

func checkResolver() *resolver.Resolver {
	return resolver.NewWithFunctions(nil)
}

func TestCheckExternal(t *testing.T) {
	srv := healthServer(t)
	ctx := context.Background()

	ok := call("health", srv.URL+"/health")
	assert.NoError(t, externalErr(ctx, ok, "", checkResolver(), nil), "a call that succeeds passes with no assertions")

	body := call("health", srv.URL+"/health")
	body.Assert = &check.Assertions{TextAssertion: check.TextAssertion{Contains: `"healthy"`}}
	assert.NoError(t, externalErr(ctx, body, "", checkResolver(), nil), "without jq the assertions read the body")

	wrong := call("health", srv.URL+"/health")
	wrong.Assert = &check.Assertions{TextAssertion: check.TextAssertion{Contains: "degraded"}}
	assert.ErrorContains(t, externalErr(ctx, wrong, "", checkResolver(), nil), `external "health"`)

	down := call("down", srv.URL+"/down")
	assert.Error(t, externalErr(ctx, down, "", checkResolver(), nil), "a failed call fails the check")

	skipped := call("gated", srv.URL+"/down")
	skipped.When = condition.Conditions{{Field: "{{ .inrun.inPod }}", Equals: "true"}}
	assert.NoError(t, externalErr(ctx, skipped, "", checkResolver(), nil), "a call whose when: fails is skipped")
}

func TestCheckExternal_JQ(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed")
	}
	srv := healthServer(t)
	c := call("health", srv.URL+"/health")
	c.JQ = ".json.state"
	c.Assert = &check.Assertions{TextAssertion: check.TextAssertion{Equals: "healthy"}}
	assert.NoError(t, externalErr(context.Background(), c, "", checkResolver(), nil))

	c.Assert = &check.Assertions{TextAssertion: check.TextAssertion{Equals: "degraded"}}
	assert.Error(t, externalErr(context.Background(), c, "", checkResolver(), nil))
}

func TestCheckExternal_UserFunctionInURL(t *testing.T) {
	srv := healthServer(t)
	resolver := resolver.NewWithFunctions(nil, resolver.Function{Name: "healthURL", Expression: srv.URL + `/health`})
	assert.NoError(t, externalErr(context.Background(), call("health", "{{ healthURL }}"), "", resolver, nil),
		"spec.functions resolve in an external call's url")
}

func TestExpandIncludes_ExternalIncludes(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "calls.yaml"), []byte(`calls:
  - name: a
    url: http://a.invalid
  - name: b
    url: http://b.invalid
`), 0o644))

	inc := spec.ExternalCheck{}
	inc.Include = "calls.yaml"
	own := call("own", "http://own.invalid")
	got, err := spec.Expectations{{Name: "s", External: []spec.ExternalCheck{inc, own}}}.ExpandIncludes(dir)
	require.NoError(t, err)
	var names []string
	for _, c := range got[0].External {
		names = append(names, c.Name)
	}
	assert.Equal(t, []string{"a", "b", "own"}, names)

	// An e2e calls file can carry each call's own checks.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "checks.yaml"), []byte(`calls:
  - name: health
    url: http://health.invalid
    jq: .json.state
    assert:
      equals: healthy
`), 0o644))
	inc.Include = "checks.yaml"
	got, err = spec.Expectations{{Name: "s", External: []spec.ExternalCheck{inc}}}.ExpandIncludes(dir)
	require.NoError(t, err)
	require.Len(t, got[0].External, 1)
	assert.Equal(t, ".json.state", got[0].External[0].JQ)
	require.NotNil(t, got[0].External[0].Assert)
	assert.Equal(t, "healthy", got[0].External[0].Assert.Equals)
}

func TestValidateExternal(t *testing.T) {
	noNamespace := call("db", "http://db.invalid")
	noNamespace.Auth = &spec.ExternalAuth{SecretRef: &spec.SecretRef{Name: "creds", Key: "token"}}

	errs := ValidateExternal([]spec.Expectation{{Name: "s", External: []spec.ExternalCheck{
		call("", "http://x.invalid"),
		call("dup", "http://x.invalid"),
		call("dup", "http://x.invalid"),
		call("nourl", ""),
		noNamespace,
	}}})
	var got []string
	for _, e := range errs {
		got = append(got, e.Error())
	}
	joined := strings.Join(got, "\n")
	assert.Contains(t, joined, "name is required")
	assert.Contains(t, joined, `duplicate call name "dup"`)
	assert.Contains(t, joined, "external[3]")
	assert.Contains(t, joined, "auth.secretRef.namespace is required")

	assert.Empty(t, ValidateExternal([]spec.Expectation{{Name: "s", External: []spec.ExternalCheck{call("ok", "http://x.invalid")}}}))
}

// externalErr is runExternal's error, for assertions on the outcome alone.
func externalErr(ctx context.Context, c spec.ExternalCheck, workDir string, resolver *resolver.Resolver, cs kubernetes.Interface) error {
	_, err := runExternal(ctx, c, workDir, resolver, cs)
	return err
}
