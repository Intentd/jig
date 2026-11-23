package plan

import (
	"bytes"
	"testing"

	"github.com/intentd/jig/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The plan lists the data services with their addresses, the operators, and
// the Secrets with their key names, never a value.
func TestPlan_Ecosystem(t *testing.T) {
	decl := &declaration{
		Name: "shop",
		Environment: &spec.Environment{
			Ecosystem: &ecosystem{
				Postgres: &spec.EcosystemPostgres{
					DataService: spec.DataService{
						User:     "shop",
						Password: "secret-value",
					},
				},
				Kafka: &spec.EcosystemKafka{
					Topics: []string{"orders"},
				},
				Secrets: &spec.EcosystemSecrets{
					Export: &spec.EcosystemExport{
						Tools:     []string{"postgres"},
						Name:      "shop-db",
						Namespace: "shop",
					},
				},
			},
		},
	}

	result, err := Build(decl)
	require.NoError(t, err)

	var b bytes.Buffer
	require.NoError(t, result.Write(&b, "text"))

	require.Error(t, result.Write(&b, "unknown"))

	out := b.String()
	for _, want := range []string{
		"ecosystem (namespace data)",
		"postgres-rw.data:5432",
		"kafka-kafka-bootstrap.data:9092",
		"cloudnative-pg 0.29.1, strimzi 1.3.1",
		"secret shop-db (shop): POSTGRES_DATABASE, POSTGRES_HOST",
	} {
		assert.Contains(t, out, want)
	}

	assert.NotContains(t, out, "secret-value", "the plan never prints a value")
}
