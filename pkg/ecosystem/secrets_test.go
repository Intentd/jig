package ecosystem

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/kubeclient/kubefake"
)

// The credentials and the export Secret are written, a missing namespace is
// created and undone, one already there is left; undoing removes it all.
func TestPlan_WriteSecrets(t *testing.T) {
	ctx := context.Background()
	cs := kubefake.NewClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shop"}})
	p, err := New(&spec.Ecosystem{
		Postgres: &spec.EcosystemPostgres{},
		Secrets:  &spec.EcosystemSecrets{Export: &spec.EcosystemExport{Name: "shop-deps", Namespace: "shop"}},
	}, fixed)
	require.NoError(t, err)

	done, err := p.WriteSecrets(ctx, cs)
	require.NoError(t, err)

	deps, err := cs.CoreV1().Secrets("shop").Get(ctx, "shop-deps", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, p.Conns[Postgres]["url"], string(deps.Data["POSTGRES_URL"]))
	for _, c := range p.Credentials {
		_, err := cs.CoreV1().Secrets(c.Namespace).Get(ctx, c.Name, metav1.GetOptions{})
		assert.NoError(t, err, "credentials %s/%s", c.Namespace, c.Name)
	}

	var labels []string
	for _, u := range done {
		labels = append(labels, u.Label)
	}
	assert.Contains(t, labels, "namespace "+DefaultNamespace, "the data namespace was created")
	assert.NotContains(t, labels, "namespace shop", "shop was there already")
	assert.Equal(t, "Secret shop/shop-deps", labels[len(labels)-1], "the outputs come last")

	for i := len(done) - 1; i >= 0; i-- {
		require.NoError(t, done[i].Do(ctx))
	}
	_, err = cs.CoreV1().Secrets("shop").Get(ctx, "shop-deps", metav1.GetOptions{})
	assert.Error(t, err)
	_, err = cs.CoreV1().Namespaces().Get(ctx, "shop", metav1.GetOptions{})
	assert.NoError(t, err, "a namespace jig didn't create stays")
}

// The context and the data views are the plan's connection details.
func TestPlan_ContextAndData(t *testing.T) {
	p, err := New(&spec.Ecosystem{Postgres: &spec.EcosystemPostgres{}}, fixed)
	require.NoError(t, err)
	url := p.Conns[Postgres]["url"]
	assert.Equal(t, url, p.Context()["postgres.url"])
	assert.Equal(t, url, p.Data()[Postgres].(map[string]any)["url"])
	assert.Empty(t, (*Plan)(nil).Context())
}
