package ecosystem

import (
	"fmt"

	"github.com/intentd/jig/pkg/ecosystem/operator"
	"github.com/intentd/jig/pkg/spec"
)

// mariadb is a MariaDB server (mariadb-operator): a user, a database and an
// ALL grant on it, reached at mariadb. It speaks the MySQL protocol.
type mariadb struct{ spec.EcosystemMariadb }

func (mariadb) name() string                 { return Mariadb }
func (s mariadb) settings() spec.DataService { return s.DataService }
func (mariadb) fields() []string {
	return []string{"host", "port", "user", "password", "database", "url"}
}

// The CRDs are their own chart, installed first: the operator chart's are off.
func (mariadb) operators() operator.Operators {
	return operator.Operators{operator.MariadbCRDs, operator.MariadbOperator}
}

func (mariadb) plan(r resolved, password func() string) part {
	root, secret := "mariadb-root", "mariadb-"+r.User
	host := "mariadb." + r.Namespace
	return part{
		Credentials: Secrets{
			{Name: root, Namespace: r.Namespace, Data: map[string]string{"password": password()}},
			{Name: secret, Namespace: r.Namespace, Data: map[string]string{"password": r.Password}},
		},
		Values: map[string]any{Mariadb: map[string]any{
			"replicas":     r.Replicas,
			"storage":      map[string]any{"size": r.Storage},
			"rootPassword": map[string]any{"secretName": root},
			"databases":    map[string]any{r.Database: map[string]any{"cleanupPolicy": "Delete"}},
			"users": map[string]any{r.User: map[string]any{
				"password": map[string]any{"secretName": secret},
				"grants":   map[string]any{r.Database: map[string]any{}},
			}},
		}},
		Conn: map[string]string{"host": host, "port": "3306", "user": r.User, "password": r.Password, "database": r.Database,
			"url": fmt.Sprintf("mysql://%s:%s@%s:3306/%s", r.User, r.Password, host, r.Database)},
		Ready: Waits{{"MariaDB", Mariadb, r.Namespace, "Ready"}},
	}
}
