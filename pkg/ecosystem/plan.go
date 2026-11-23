// Package ecosystem installs the data services a jig declares
// (environment.ecosystem): the operators they need, the data-services chart
// with values built from the declaration, and the Secrets that carry each
// service's connection details to the workload.
//
// Names and addresses are fixed, one instance per service named for it, and
// jig generates every password, so the connection details are known before
// anything is installed: nothing is read back from the cluster.
//
// Each data service is one implementation (postgres.go, mariadb.go, …); New
// runs them all, then writes the Secrets.
package ecosystem

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/intentd/jig/pkg/ecosystem/operator"
	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/kel/core"
)

// The services. Their order is only for stable output (the same plan,
// operators and export keys every run): no service depends on another.
const (
	Postgres = "postgres"
	Mariadb  = "mariadb"
	Rabbitmq = "rabbitmq"
	Redis    = "redis"
	Kafka    = "kafka"
)

// Services are the services the ecosystem knows, in order.
var Services = []string{Postgres, Mariadb, Rabbitmq, Redis, Kafka}

// Defaults for what a declaration leaves out.
const (
	DefaultNamespace = "data"
	defaultUser      = "app"
	defaultDatabase  = "app"
	defaultStorage   = "1Gi"
	defaultSecret    = "ecosystem"
	defaultSecretNS  = "default"
)

// Secret is a Secret the ecosystem writes.
type Secret struct {
	Name, Namespace string
	Type            string // "" is Opaque
	Labels          map[string]string
	Data            map[string]string
}

// Secrets are Secrets the ecosystem writes.
type Secrets []Secret

// Wait is a resource the ecosystem waits on: until its condition is True.
type Wait struct {
	Kind, Name, Namespace, Condition string
}

// Waits are resources to wait on, in order.
type Waits []Wait

// Plan is what installing an ecosystem does, worked out without a cluster.
type Plan struct {
	// Namespaces are the namespaces the instances and Secrets need.
	Namespaces []string
	// Operators the declared services need, in install order.
	Operators operator.Operators
	// Credentials are the password Secrets the chart's entries name, written
	// before the chart is installed.
	Credentials Secrets
	// Values are the data-services chart's values, by service.
	Values map[string]any
	// Ready are the instances to wait on after the chart is installed.
	Ready Waits
	// Conns are each declared service's connection details, by field.
	Conns map[string]map[string]string
	// Outputs are the export and define Secrets, written last.
	Outputs Secrets
}

// service is one data service: its connection fields, the operators it
// needs, and what it adds to a plan.
type service interface {
	name() string
	settings() spec.DataService
	fields() []string
	operators() operator.Operators
	plan(r resolved, password func() string) part
}

// resolved are a service's settings with the defaults filled in.
type resolved struct {
	Namespace, User, Database, Password, Storage string
	Replicas                                     int
}

// part is what one service adds to a plan.
type part struct {
	Credentials Secrets
	Values      map[string]any
	Conn        map[string]string
	Ready       Waits
}

// declared are e's services, in Services order.
func declared(e *spec.Ecosystem) []service {
	var out []service
	if e.Postgres != nil {
		out = append(out, postgres{*e.Postgres})
	}
	if e.Mariadb != nil {
		out = append(out, mariadb{*e.Mariadb})
	}
	if e.Rabbitmq != nil {
		out = append(out, rabbitmq{*e.Rabbitmq})
	}
	if e.Redis != nil {
		out = append(out, redis{*e.Redis})
	}
	if e.Kafka != nil {
		out = append(out, kafka{*e.Kafka})
	}
	return out
}

// New works out the plan for e: each declared service's part, the operators
// they need, then the Secrets. password generates a password where the
// declaration sets none.
func New(e *spec.Ecosystem, password func() string) (*Plan, error) {
	if e == nil {
		return nil, nil
	}
	p := &Plan{Values: map[string]any{}, Conns: map[string]map[string]string{}}
	namespaces := map[string]bool{}
	fields := map[string][]string{}

	for _, s := range declared(e) {
		r := resolve(s.settings(), e.Namespace, password)
		namespaces[r.Namespace] = true
		part := s.plan(r, password)
		p.Credentials = append(p.Credentials, part.Credentials...)
		p.Values[s.name()] = part.Values
		p.Conns[s.name()] = part.Conn
		p.Ready = append(p.Ready, part.Ready...)
		fields[s.name()] = s.fields()
		for _, op := range s.operators() {
			if !slices.ContainsFunc(p.Operators, func(o operator.Operator) bool { return o.Name == op.Name }) {
				p.Operators = append(p.Operators, op)
			}
		}
	}

	if e.Secrets != nil {
		out, err := outputs(e.Secrets, p.Conns, fields)
		if err != nil {
			return nil, err
		}
		p.Outputs = out
		for _, o := range out {
			namespaces[o.Namespace] = true
		}
	}
	p.Namespaces = sorted(keys(namespaces))
	return p, nil
}

// resolve fills in a service's defaults: the ecosystem's namespace, user and
// database "app", a generated password, 1Gi, one replica.
func resolve(d spec.DataService, namespace string, password func() string) resolved {
	r := resolved{
		Namespace: or(d.Namespace, or(namespace, DefaultNamespace)),
		User:      or(d.User, defaultUser),
		Database:  or(d.Database, defaultDatabase),
		Password:  d.Password,
		Storage:   or(d.Storage, defaultStorage),
		Replicas:  d.Replicas,
	}
	if r.Password == "" {
		r.Password = password()
	}
	if r.Replicas == 0 {
		r.Replicas = 1
	}
	return r
}

// outputs builds the export and define Secrets from the connection details.
func outputs(s *spec.EcosystemSecrets, conns map[string]map[string]string, fields map[string][]string) (Secrets, error) {
	var out Secrets
	if x := s.Export; x != nil {
		tools := x.Tools
		if len(tools) == 0 {
			for _, svc := range Services {
				if conns[svc] != nil {
					tools = append(tools, svc)
				}
			}
		}
		sec := Secret{Name: or(x.Name, defaultSecret), Namespace: or(x.Namespace, defaultSecretNS), Data: map[string]string{}}
		for _, tool := range tools {
			c, ok := conns[tool]
			if !ok {
				return nil, fmt.Errorf("secrets.export: %q is not declared in the ecosystem", tool)
			}
			for _, f := range fields[tool] {
				sec.Data[strings.ToUpper(tool+"_"+f)] = c[f]
			}
		}
		out = append(out, sec)
	}
	for _, name := range sorted(keys(s.Define)) {
		d := s.Define[name]
		sec := Secret{Name: name, Namespace: or(d.Namespace, defaultSecretNS), Data: map[string]string{}}
		for key, ref := range d.Keys {
			v, err := lookup(conns, fields, ref)
			if err != nil {
				return nil, fmt.Errorf("secrets.define.%s.keys.%s: %w", name, key, err)
			}
			sec.Data[key] = v
		}
		out = append(out, sec)
	}
	return out, nil
}

// lookup resolves a tool.field reference.
func lookup(conns map[string]map[string]string, fields map[string][]string, ref string) (string, error) {
	tool, field, ok := strings.Cut(ref, ".")
	if !ok {
		return "", fmt.Errorf("%q: want tool.field, such as postgres.url", ref)
	}
	c, ok := conns[tool]
	if !ok {
		return "", fmt.Errorf("%q: %s is not declared in the ecosystem", ref, tool)
	}
	v, ok := c[field]
	if !ok {
		return "", fmt.Errorf("%q: %s has no %s (it has %s)", ref, tool, field, strings.Join(fields[tool], ", "))
	}
	return v, nil
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	sort.Strings(out)
	return out
}

// Context returns every declared service's connection details keyed
// tool.field (postgres.url, kafka.bootstrap): the ecosystem context, as
// ${{ ecosystem.postgres.url }} reads it.
func (p *Plan) Context() map[string]string {
	out := map[string]string{}
	if p == nil {
		return out
	}
	for tool, conn := range p.Conns {
		for field, v := range conn {
			out[tool+"."+field] = v
		}
	}
	return out
}

// Data returns the connection details by service, then field: what
// templates ({{ .ecosystem.postgres.url }}) and CEL read under ecosystem.
func (p *Plan) Data() map[string]any {
	out := map[string]any{}
	if p == nil {
		return out
	}
	for tool, conn := range p.Conns {
		m := make(map[string]any, len(conn))
		for field, v := range conn {
			m[field] = v
		}
		out[tool] = m
	}
	return out
}

// Password returns a new random password: 24 characters from [a-zA-Z0-9]
// (KEL's randomAlphanumeric), which a URL and a shell carry as they are, so
// a connection URL needs no escaping.
func Password() string {
	p, err := core.RandomAlphanumeric(24)
	if err != nil {
		panic(fmt.Sprintf("ecosystem: %v", err))
	}
	return p
}

// Placeholder is the password a plan shows offline: validate builds the
// plan with it, so every reference resolves and no value is invented.
func Placeholder() string { return "<generated>" }
