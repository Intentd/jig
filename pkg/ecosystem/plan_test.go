package ecosystem

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intentd/jig/pkg/spec"
)

func fixed() string { return "pw" }

// Every service with its defaults: one instance each in "data", the
// connection details known from the names, the operators each needs once,
// and an instance to wait on per service and Kafka topic.
func TestNew_Defaults(t *testing.T) {
	p, err := New(&spec.Ecosystem{
		Postgres: &spec.EcosystemPostgres{},
		Mariadb:  &spec.EcosystemMariadb{},
		Rabbitmq: &spec.EcosystemRabbitmq{},
		Redis:    &spec.EcosystemRedis{},
		Kafka:    &spec.EcosystemKafka{Topics: []string{"orders"}},
	}, fixed)
	require.NoError(t, err)

	assert.Equal(t, []string{"data"}, p.Namespaces)
	assert.Equal(t, "postgresql://app:pw@postgres-rw.data:5432/app", p.Conns[Postgres]["url"])
	assert.Equal(t, "mysql://app:pw@mariadb.data:3306/app", p.Conns[Mariadb]["url"])
	assert.Equal(t, "amqp://app:pw@rabbitmq.data:5672/app", p.Conns[Rabbitmq]["url"])
	assert.Equal(t, "redis://default:pw@redis.data:6379", p.Conns[Redis]["url"])
	assert.Equal(t, "kafka-kafka-bootstrap.data:9092", p.Conns[Kafka]["bootstrap"])

	var ops []string
	for _, o := range p.Operators {
		ops = append(ops, o.Name)
	}
	assert.Equal(t, []string{
		"cloudnative-pg", "mariadb-operator-crds", "mariadb-operator",
		"cert-manager", "rabbitmq-cluster-operator", "rabbitmq-topology-operator",
		"redis-operator", "strimzi",
	}, ops)

	assert.Contains(t, p.Ready, Wait{"RabbitmqCluster", "rabbitmq", "data", "AllReplicasReady"})
	assert.Contains(t, p.Ready, Wait{"KafkaTopic", "orders", "data", "Ready"})
}

// The chart's values name the Secrets jig writes, with the keys each
// template reads.
func TestNew_ValuesAndCredentials(t *testing.T) {
	p, err := New(&spec.Ecosystem{
		Namespace: "deps",
		Postgres:  &spec.EcosystemPostgres{DataService: spec.DataService{User: "shop", Database: "orders", Password: "given"}},
	}, fixed)
	require.NoError(t, err)

	pg := p.Values[Postgres].(map[string]any)[Postgres].(map[string]any)
	assert.Equal(t, map[string]any{"login": true, "password": map[string]any{"secretName": "postgres-shop"}},
		pg["roles"].(map[string]any)["shop"])
	assert.Equal(t, "shop", pg["databases"].(map[string]any)["orders"].(map[string]any)["owner"])

	require.Len(t, p.Credentials, 1)
	c := p.Credentials[0]
	assert.Equal(t, "postgres-shop", c.Name)
	assert.Equal(t, "deps", c.Namespace)
	assert.Equal(t, "kubernetes.io/basic-auth", c.Type)
	assert.Equal(t, map[string]string{"username": "shop", "password": "given"}, c.Data, "a declared password is used as is")
}

// export writes every field of the listed tools as TOOL_FIELD; define maps
// the app's own keys to tool.field.
func TestNew_Outputs(t *testing.T) {
	p, err := New(&spec.Ecosystem{
		Mariadb: &spec.EcosystemMariadb{},
		Kafka:   &spec.EcosystemKafka{},
		Secrets: &spec.EcosystemSecrets{
			Export: &spec.EcosystemExport{Tools: []string{"kafka"}, Name: "deps", Namespace: "shop"},
			Define: map[string]spec.EcosystemDefine{
				"shop-db": {Namespace: "shop", Keys: map[string]string{"MYSQL_HOST": "mariadb.host", "DATABASE_URL": "mariadb.url"}},
			},
		},
	}, fixed)
	require.NoError(t, err)
	require.Len(t, p.Outputs, 2)
	assert.Equal(t, Secret{Name: "deps", Namespace: "shop", Data: map[string]string{"KAFKA_BOOTSTRAP": "kafka-kafka-bootstrap.data:9092"}}, p.Outputs[0])
	assert.Equal(t, map[string]string{"MYSQL_HOST": "mariadb.data", "DATABASE_URL": "mysql://app:pw@mariadb.data:3306/app"}, p.Outputs[1].Data)
	assert.Equal(t, []string{"data", "shop"}, p.Namespaces)
}

func TestNew_BadReferences(t *testing.T) {
	for name, tc := range map[string]struct {
		secrets *spec.EcosystemSecrets
		want    string
	}{
		"export undeclared":     {&spec.EcosystemSecrets{Export: &spec.EcosystemExport{Tools: []string{"redis"}}}, `"redis" is not declared`},
		"define not tool.field": {&spec.EcosystemSecrets{Define: map[string]spec.EcosystemDefine{"x": {Keys: map[string]string{"K": "kafka"}}}}, "want tool.field"},
		"define unknown field":  {&spec.EcosystemSecrets{Define: map[string]spec.EcosystemDefine{"x": {Keys: map[string]string{"K": "kafka.url"}}}}, "kafka has no url"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(&spec.Ecosystem{Kafka: &spec.EcosystemKafka{}, Secrets: tc.secrets}, fixed)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}
