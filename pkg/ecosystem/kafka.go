package ecosystem

import (
	"github.com/intentd/jig/pkg/ecosystem/operator"
	"github.com/intentd/jig/pkg/spec"
)

// kafka is a Kafka cluster (Strimzi, KRaft) with its topics, reached at
// kafka-kafka-bootstrap on the plain listener: a test cluster, no auth.
type kafka struct{ spec.EcosystemKafka }

func (kafka) name() string                  { return Kafka }
func (s kafka) settings() spec.DataService  { return s.DataService }
func (kafka) fields() []string              { return []string{"bootstrap"} }
func (kafka) operators() operator.Operators { return operator.Operators{operator.Strimzi} }

func (s kafka) plan(r resolved, _ func() string) part {
	topics := map[string]any{}
	ready := Waits{{"Kafka", Kafka, r.Namespace, "Ready"}}
	for _, t := range sorted(s.Topics) {
		topics[t] = map[string]any{}
		ready = append(ready, Wait{"KafkaTopic", t, r.Namespace, "Ready"})
	}
	return part{
		Values: map[string]any{Kafka: map[string]any{
			"replicas": r.Replicas,
			"storage":  map[string]any{"size": r.Storage, "deleteClaim": true},
			"topics":   topics,
		}},
		Conn:  map[string]string{"bootstrap": "kafka-kafka-bootstrap." + r.Namespace + ":9092"},
		Ready: ready,
	}
}
