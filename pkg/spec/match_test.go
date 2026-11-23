package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"

	tassert "github.com/intentd/tools/assert"
)

// match is flattened into json: assertions, one per leaf in key order: a
// literal is equals, ${ … } or {{ … }} is expr:, lists element by element.
func TestMatchAssertions(t *testing.T) {
	got := MatchAssertions(map[string]any{
		"spec": map[string]any{
			"replicas": 2,
			"template": map[string]any{"spec": map[string]any{"containers": []any{
				map[string]any{"name": "api", "image": `${ value.startsWith("shop-api:") }`},
			}}},
		},
		"status": map[string]any{"ready": true},
	})
	assert.Equal(t, tassert.JSONAssertions{
		{Path: ".spec.replicas", TextAssertion: tassert.TextAssertion{Equals: "2"}},
		{Path: ".spec.template.spec.containers[0].image", TextAssertion: tassert.TextAssertion{Expr: `${ value.startsWith("shop-api:") }`}},
		{Path: ".spec.template.spec.containers[0].name", TextAssertion: tassert.TextAssertion{Equals: "api"}},
		{Path: ".status.ready", TextAssertion: tassert.TextAssertion{Equals: "true"}},
	}, got)

	// The flattened checks pass on an object with more than match asks.
	obj := `{"spec":{"replicas":2,"paused":false,"template":{"spec":{"containers":[{"name":"api","image":"shop-api:dev"},{"name":"side"}]}}},"status":{"ready":true}}`
	assert.NoError(t, tassert.Assertions{JSON: got}.Check(obj))
}
