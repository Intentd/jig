package ecosystem

import (
	"fmt"

	"github.com/intentd/jig/pkg/ecosystem/operator"
	"github.com/intentd/jig/pkg/spec"
)

// redis is a standalone Redis (OT Redis Operator) with a password, reached at
// redis.
type redis struct{ spec.EcosystemRedis }

func (redis) name() string                  { return Redis }
func (s redis) settings() spec.DataService  { return s.DataService }
func (redis) fields() []string              { return []string{"host", "port", "password", "url"} }
func (redis) operators() operator.Operators { return operator.Operators{operator.Redis} }

func (redis) plan(r resolved, _ func() string) part {
	secret := "redis-password"
	host := "redis." + r.Namespace
	return part{
		Credentials: Secrets{{Name: secret, Namespace: r.Namespace, Data: map[string]string{"password": r.Password}}},
		Values: map[string]any{Redis: map[string]any{
			"storage":  map[string]any{"size": r.Storage},
			"password": map[string]any{"secretName": secret},
		}},
		Conn: map[string]string{"host": host, "port": "6379", "password": r.Password,
			// The default user, named: Redis 7 reads redis://:pw@ as an
			// empty username and refuses it.
			"url": fmt.Sprintf("redis://default:%s@%s:6379", r.Password, host)},
		// The operator's Redis has no Ready condition: its pod does.
		Ready: Waits{{"Pod", "redis-0", r.Namespace, "Ready"}},
	}
}
