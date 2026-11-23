package ecosystem

import (
	"fmt"

	"github.com/intentd/jig/pkg/ecosystem/operator"
	"github.com/intentd/jig/pkg/spec"
)

// postgres is a PostgreSQL cluster (CloudNativePG): a login role and a
// database it owns, reached at postgres-rw.
type postgres struct{ spec.EcosystemPostgres }

func (postgres) name() string                 { return Postgres }
func (s postgres) settings() spec.DataService { return s.DataService }
func (postgres) fields() []string {
	return []string{"host", "port", "user", "password", "database", "url"}
}
func (postgres) operators() operator.Operators { return operator.Operators{operator.CloudNativePG} }

func (postgres) plan(r resolved, _ func() string) part {
	secret := "postgres-" + r.User
	host := "postgres-rw." + r.Namespace
	return part{
		Credentials: Secrets{{Name: secret, Namespace: r.Namespace, Type: "kubernetes.io/basic-auth",
			Data: map[string]string{"username": r.User, "password": r.Password}}},
		Values: map[string]any{Postgres: map[string]any{
			"instances": r.Replicas,
			"storage":   map[string]any{"size": r.Storage},
			"roles":     map[string]any{r.User: map[string]any{"login": true, "password": map[string]any{"secretName": secret}}},
			"databases": map[string]any{r.Database: map[string]any{"owner": r.User, "reclaimPolicy": "delete"}},
		}},
		Conn: map[string]string{"host": host, "port": "5432", "user": r.User, "password": r.Password, "database": r.Database,
			"url": fmt.Sprintf("postgresql://%s:%s@%s:5432/%s", r.User, r.Password, host, r.Database)},
		Ready: Waits{{"Cluster", Postgres, r.Namespace, "Ready"}},
	}
}
