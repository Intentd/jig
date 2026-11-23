package ecosystem

import (
	"fmt"

	"github.com/intentd/jig/pkg/ecosystem/operator"
	"github.com/intentd/jig/pkg/spec"
)

// rabbitmq is a RabbitMQ cluster (Cluster and Messaging Topology Operators):
// its vhosts and a user with full permissions on each, reached at rabbitmq.
type rabbitmq struct{ spec.EcosystemRabbitmq }

const defaultVhost = "app"

func (rabbitmq) name() string                 { return Rabbitmq }
func (s rabbitmq) settings() spec.DataService { return s.DataService }
func (rabbitmq) fields() []string {
	return []string{"host", "port", "user", "password", "vhost", "url"}
}

// cert-manager first: both operators' webhooks get their certificates from it.
func (rabbitmq) operators() operator.Operators {
	return operator.Operators{operator.CertManager, operator.RabbitmqCluster, operator.RabbitmqTopology}
}

func (s rabbitmq) plan(r resolved, _ func() string) part {
	vhosts := s.Vhosts
	if len(vhosts) == 0 {
		vhosts = []string{defaultVhost}
	}
	secret := "rabbitmq-" + r.User
	perms := map[string]any{}
	for _, v := range vhosts {
		perms[v] = map[string]any{"configure": ".*", "write": ".*", "read": ".*"}
	}
	host := "rabbitmq." + r.Namespace
	return part{
		Credentials: Secrets{{Name: secret, Namespace: r.Namespace,
			Labels: map[string]string{"rabbitmq.com/topology-operator": "true"},
			Data:   map[string]string{"username": r.User, "password": r.Password}}},
		Values: map[string]any{Rabbitmq: map[string]any{
			"replicas":    r.Replicas,
			"persistence": map[string]any{"storage": r.Storage},
			"vhosts":      vhosts,
			"users": map[string]any{r.User: map[string]any{
				"credentials": map[string]any{"secretName": secret},
				"permissions": perms,
			}},
		}},
		Conn: map[string]string{"host": host, "port": "5672", "user": r.User, "password": r.Password, "vhost": vhosts[0],
			"url": fmt.Sprintf("amqp://%s:%s@%s:5672/%s", r.User, r.Password, host, vhosts[0])},
		Ready: Waits{{"RabbitmqCluster", Rabbitmq, r.Namespace, "AllReplicasReady"}},
	}
}
