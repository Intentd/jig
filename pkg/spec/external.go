package spec

import (
	"context"
	"fmt"
	"os"

	"github.com/intentd/tools/condition"
	"github.com/intentd/tools/external"
	"github.com/intentd/tools/kube"
	"github.com/intentd/tools/resolver"
	"k8s.io/client-go/kubernetes"
)

// ExternalCall is an external check's call: HTTP, Prometheus, Postgres,
// Redis, Mongo or Kafka, made from the machine running the test.
type ExternalCall struct {
	// Name labels the call in messages and results.
	Name string `yaml:"name"`
	// Protocol is http (the default), prometheus, postgres, redis, mongo or kafka.
	Protocol external.Protocol `yaml:"protocol,omitempty"`
	// URL is the endpoint or connection string; a template.
	URL string `yaml:"url"`
	// Method is the HTTP method; GET by default.
	Method string `yaml:"method,omitempty"`
	// Headers are sent with an HTTP request; values are templates.
	Headers map[string]string `yaml:"headers,omitempty"`
	// Body is an HTTP request's body; a template.
	Body string `yaml:"body,omitempty"`
	// Query is the protocol's query: PromQL, SQL, a Redis command, a Mongo
	// "db.collection", a Kafka "group/topic" or "@topic"; a template.
	Query string `yaml:"query,omitempty"`
	// ExpectedStatus are the HTTP statuses that succeed; empty: below 400.
	ExpectedStatus []int `yaml:"expectedStatus,omitempty"`
	// Timeout bounds the call ("10s" by default).
	Timeout string `yaml:"timeout,omitempty"`
	// CacheFor keeps a result for this long.
	CacheFor string `yaml:"cacheFor,omitempty"`
	// ContinueOnError records a failed call's result instead of failing.
	ContinueOnError bool `yaml:"continueOnError,omitempty"`
	// When and Or decide whether the call is made.
	When condition.Conditions `yaml:"when,omitempty"`
	Or   condition.Conditions `yaml:"or,omitempty"`
	// Auth sends a bearer token from a Secret or an environment variable.
	Auth *ExternalAuth `yaml:"auth,omitempty"`
	// Include is a file of external checks, expanded in place of this one.
	Include string `yaml:"include,omitempty"`
}

// ExternalAuth is where an external call's bearer token comes from: a
// Secret or an environment variable, exactly one.
type ExternalAuth struct {
	SecretRef *SecretRef `yaml:"secretRef,omitempty"`
	Env       string     `yaml:"env,omitempty"`
	// Header carries the token; Authorization by default.
	Header string `yaml:"header,omitempty"`
}

// SecretRef is a key of a Secret.
type SecretRef struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Key       string `yaml:"key"`
}

// call is c as tools/external makes it, without its credential.
func (c ExternalCall) call() external.Call {
	out := external.Call{
		Name: c.Name, Protocol: c.Protocol,
		URL: c.URL, Method: c.Method, Headers: c.Headers,
		Body: c.Body, Query: c.Query, ExpectedStatus: c.ExpectedStatus,
		Timeout: c.Timeout, CacheFor: c.CacheFor, ContinueOnError: c.ContinueOnError,
		When: c.When, Or: c.Or,
	}
	if c.Auth != nil {
		out.AuthHeader = c.Auth.Header
	}
	return out
}

// ValidateExternalCall checks c's shape, labelling errors with location.
func ValidateExternalCall(location string, c ExternalCall) error {
	if err := external.Validate(location, c.call()); err != nil {
		return err
	}
	a := c.Auth
	if a == nil {
		return nil
	}
	switch {
	case a.SecretRef == nil && a.Env == "":
		return fmt.Errorf("%s: auth: exactly one of secretRef or env must be set", location)
	case a.SecretRef != nil && a.Env != "":
		return fmt.Errorf("%s: auth: secretRef and env are mutually exclusive", location)
	case a.SecretRef != nil && a.SecretRef.Name == "":
		return fmt.Errorf("%s: auth.secretRef.name is required", location)
	case a.SecretRef != nil && a.SecretRef.Namespace == "":
		return fmt.Errorf("%s: auth.secretRef.namespace is required", location)
	case a.SecretRef != nil && a.SecretRef.Key == "":
		return fmt.Errorf("%s: auth.secretRef.key is required", location)
	}
	return nil
}

// CallExternal makes c with tmpl, its credential read from cs (a secretRef)
// or the environment, and returns the result.
func CallExternal(ctx context.Context, where string, tmpl *resolver.Resolver, c ExternalCall, cs kubernetes.Interface) (map[string]any, error) {
	call := c.call()
	if c.Auth != nil && condition.Evaluate(tmpl.Data(), c.When, c.Or, tmpl.Evaluator()) {
		switch {
		case c.Auth.SecretRef != nil:
			v, err := kube.ReadSecretKey(ctx, cs, c.Auth.SecretRef.Namespace, c.Auth.SecretRef.Name, c.Auth.SecretRef.Key)
			if err != nil {
				return nil, fmt.Errorf("%s %q: auth.secretRef: %w", where, c.Name, err)
			}
			call.Credential = v
		case c.Auth.Env != "":
			call.Credential = os.Getenv(c.Auth.Env)
			if call.Credential == "" {
				return nil, fmt.Errorf("%s %q: auth.env: %q is not set or empty", where, c.Name, c.Auth.Env)
			}
		}
	}
	return external.Do(ctx, tmpl, call, external.Options{Where: where, Scope: "jig"})
}
