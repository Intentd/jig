package check

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/structured/structuredtest"
)

func read(t *testing.T, doc string) *spec.Declaration {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jig.yaml")
	structuredtest.WriteFile(t, path, doc)
	s, err := spec.Read(path)
	// if err != nil {
	// 	t.Logf("input YAML:\n%s", doc)
	// }
	require.NoError(t, err)
	return s
}

func TestDeclaration(t *testing.T) {
	for name, tc := range map[string]struct{ doc, want string }{
		"missing apply file":               {"name: x\nprepare: [{name: a, apply: ./nope.yaml}]\n", `prepare[0].apply[0] "./nope.yaml"`},
		"missing delete file":              {"name: x\nprepare: [{name: d, delete: ./gone.yaml}]\n", `prepare[0].delete[0] "./gone.yaml"`},
		"no cluster, kubectl":              {"name: x\nenvironment: {cluster: {provider: none}}\nexpect: [{name: e, kubectl: {get: [{kind: Pod, name: p}]}}]\n", "kubectl needs a cluster"},
		"no cluster, apply":                {"name: x\nenvironment: {cluster: {provider: none}}\nprepare: [{name: a, apply: ./x.yaml}]\n", "only commands run without a cluster"},
		"no cluster, resources":            {"name: x\nenvironment: {cluster: {provider: none}}\nexpect: [{name: e, resources: [{kind: Pod}]}]\n", "resources need a cluster"},
		"no cluster, external":             {"name: x\nenvironment: {cluster: {provider: none}}\nexpect: [{name: e, external: [{name: c, url: http://x}]}]\n", "external needs a cluster"},
		"unknown provider":                 {"name: x\nenvironment: {cluster: {provider: eks}}\n", `provider "eks"`},
		"external no url":                  {"name: x\nexpect: [{name: e, external: [{name: c}]}]\n", "url is required"},
		"external no name":                 {"name: x\nexpect: [{name: e, external: [{url: http://x}]}]\n", "name is required"},
		"bad image":                        {"name: x\nenvironment: {images: [{image: 'Not An Image'}]}\n", "is invalid"},
		"empty finally":                    {"name: x\nfinally: {}\n", "finally: block is empty"},
		"ecosystem bad ref":                {"name: x\nenvironment:\n  ecosystem:\n    kafka: {}\n    secrets: {define: {s: {keys: {K: kafka.url}}}}\n", "environment.ecosystem.secrets.define.s.keys.K"},
		"the shape too":                    {"name: x\nexpect: [{name: e, after: nowhere}]\n", `after "nowhere"`},
		"ecosystem unknown field":          {"name: x\nenvironment: {ecosystem: {postgres: {}}}\nexpect: [{name: e, commands: [{run: 'psql ${{ ecosystem.postgres.uri }}'}]}]\n", "unknown ${{ ecosystem.postgres.uri }}: it has postgres.database"},
		"ecosystem not declared":           {"name: x\nexpect: [{name: e, commands: [{run: 'redis-cli -u ${{ ecosystem.redis.url }}'}]}]\n", "unknown ${{ ecosystem.redis.url }}: nothing is declared there"},
		"needs unknown":                    {"name: x\nenvironment: {images: [{image: 'a:dev', deploy: {ports: [80]}, needs: [b]}]}\n", `needs "b", which is not an image here`},
		"needs not deployed":               {"name: x\nenvironment: {images: [{image: 'a:dev', deploy: {ports: [80]}, needs: [b]}, {image: 'b:dev'}]}\n", `needs "b", which is not deployed`},
		"needs cycle":                      {"name: x\nenvironment: {images: [{image: 'a:dev', deploy: {ports: [80]}, needs: [b]}, {image: 'b:dev', deploy: {ports: [80]}, needs: [a]}]}\n", "needs form a cycle: a → b → a"},
		"built twice":                      {"name: x\nenvironment: {images: [{image: 'api:dev', build: {context: .}}, {image: 'api:dev', build: {context: ./other}}]}\n", `builds "api:dev", which images[0] builds too`},
		"needs a base, a row not deployed": {"name: x\nenvironment: {images: [{name: 'api-{{ .item }}', forEach: {items: [a, b]}, image: 'api:{{ .item }}'}, {image: 'web:dev', deploy: {ports: [80]}, needs: [api]}]}\n", `needs "api-a", which is not deployed`},
		"match with field":                 {"name: x\nexpect: [{name: e, kubectl: {get: [{kind: Pod, name: p, field: .x, match: {spec: {a: 1}}}]}}]\n", "match checks the whole object"},
		"match bad cel":                    {"name: x\nexpect: [{name: e, kubectl: {get: [{kind: Pod, name: p, match: {spec: {replicas: '${ value >= }'}}}]}}]\n", "match .spec.replicas"},
		"objects without sel":              {"name: x\nexpect: [{name: e, kubectl: {get: [{kind: Pod, name: p, objects: any, match: {spec: {a: 1}}}]}}]\n", "objects goes with a labelSelector"},
		"bad expr assertion":               {"name: x\nexpect: [{name: e, commands: [{run: 'echo 3', expr: '${ value > }'}]}]\n", "expr ${ value > }"},
		"bare expr":                        {"name: x\nexpect: [{name: e, commands: [{run: 'echo 3', expr: 'value > 2'}]}]\n", "write ${ … } (CEL) or {{ … }}"},
		"bad stays":                        {"name: x\nexpect: [{name: e, stays: soon, commands: [{run: 'true'}]}]\n", `stays "soon"`},
		"order of one":                     {"name: x\nexpect: [{name: e, order: [{kind: Job, name: a, at: .status.completionTime}]}]\n", "order needs two objects"},
		"guard of two kinds":               {"name: x\nguards: [{name: g, events: {namespaces: [a]}, restarts: {namespaces: [a]}}]\n", "set one of events, restarts and logs"},
		"guard logs":                       {"name: x\nguards: [{name: g, logs: {labelSelector: app=a}}]\n", "logs.labelSelector and logs.notContains are required"},
		"snapshot by selector":             {"name: x\nexpect: [{name: e, kubectl: {get: [{kind: Pod, labelSelector: app=a, snapshot: ./s.yaml}]}}]\n", "snapshot needs one object"},
		"git without build":                {"name: x\nenvironment: {images: [{image: 'a:dev', git: {url: 'https://github.com/acme/a.git'}}]}\n", "an image from git is built there; set build"},
		"git pull on local":                {"name: x\nenvironment: {images: [{image: 'a:dev', build: {context: .}, git: {url: ./a, pull: Always}}]}\n", "nothing is cloned or pulled"},
		"git bad pull":                     {"name: x\nenvironment: {images: [{image: 'a:dev', build: {context: .}, git: {url: 'https://github.com/acme/a.git', pull: Never}}]}\n", `want Always or IfNotPresent`},
		"template unknown func":            {"name: x\nexpect: [{name: e, when: [{field: '{{ nope .spec }}', equals: x}], commands: [{run: 'true'}]}]\n", `function "nope" not defined`},
	} {
		t.Run(name, func(t *testing.T) {
			errs := Validate(read(t, tc.doc))
			require.NotEmpty(t, errs)
			var msgs []string
			for _, e := range errs {
				msgs = append(msgs, e.Error())
			}
			assert.Contains(t, msgs[0]+"\n"+joined(msgs), tc.want)
		})
	}
}

// Without a cluster, every check that needs one is reported, each once.
func TestDeclaration_NoClusterReportsEveryViolation(t *testing.T) {
	errs := Validate(read(t, `name: x
environment: {cluster: {provider: none}}
expect:
  - {name: a, kubectl: {get: [{kind: Pod, name: p}]}}
  - {name: b, resources: [{kind: Pod}]}
  - {name: c, external: [{name: x, url: http://x}]}
`))
	text := joined(messages(errs))
	for _, want := range []string{`expect[0] "a": kubectl`, `expect[1] "b": resources`, `expect[2] "c": external`} {
		assert.Contains(t, text, want)
	}
}

func messages(errs []error) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Error()
	}
	return out
}

// A file the caller generates at run time is not looked for offline.
func TestDeclaration_SkipsGenerated(t *testing.T) {
	s := read(t, "name: x\nprepare: [{name: bundle, apply: ./bundle.yaml}]\n")
	require.NotEmpty(t, Validate(s))
	s.Prepare[0].Apply[0].Generated = true
	assert.Empty(t, Validate(s))
}

func joined(msgs []string) string {
	out := ""
	for _, m := range msgs {
		out += m + "\n"
	}
	return out
}

// Build paths are checked as Compose resolves them: the context against the
// test's directory, the dockerfile against the context.
func TestDeclaration_ImageBuildPaths(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"app/Dockerfile", "app/docker/Dockerfile.dev", "Dockerfile.root"} {
		structuredtest.WriteFile(t, filepath.Join(dir, f), "")
	}
	for _, tc := range []struct {
		name  string
		build cluster.Build
		want  string // substring of the single error; "" means valid
	}{
		{"default dockerfile in the context", cluster.Build{Context: "./app"}, ""},
		{"dockerfile relative to the context", cluster.Build{Context: "./app", Dockerfile: "docker/Dockerfile.dev"}, ""},
		{"dockerfile outside the context", cluster.Build{Context: "./app", Dockerfile: "../Dockerfile.root"}, ""},
		{"absolute dockerfile", cluster.Build{Context: "./app", Dockerfile: filepath.Join(dir, "Dockerfile.root")}, ""},
		{
			// Relative to the test's directory it exists, relative to the context it does not.
			"dockerfile is not resolved against the test's directory",
			cluster.Build{Context: "./app", Dockerfile: "Dockerfile.root"},
			`environment.images[0].build.dockerfile "` + filepath.Join(dir, "app", "Dockerfile.root") + `"`,
		},
		{
			"missing context names the field and the path as written",
			cluster.Build{Context: "./nope", Dockerfile: filepath.Join(dir, "Dockerfile.root")},
			`environment.images[0].build.context "./nope"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.build
			s := &spec.Declaration{Name: "x", Dir: dir, Environment: &spec.Environment{Images: cluster.Images{{Image: "app:1", Build: &b}}}}
			errs := paths(s)
			if tc.want == "" {
				assert.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1)
			assert.Contains(t, errs[0].Error(), tc.want)
		})
	}
}

// exists: false asks for an absent field: a get with only that is checked,
// as the relocation example's gets are.
func TestDeclaration_ExistsFalseIsAnAssertion(t *testing.T) {
	s := read(t, `name: x
expect:
  - name: the old field is gone
    kubectl:
      get:
        - kind: AppConfig
          name: api-server
          field: .spec.replicas
          exists: false
`)
	assert.Empty(t, Validate(s))
}
