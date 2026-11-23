// Package operator installs the Kubernetes operators jig's data services
// need: each a Helm chart or a release manifest, pinned per jig release.
// An operator whose CRD is already in the cluster is taken as running and
// left alone.
package operator

import "github.com/intentd/tools/cluster"

// Operator is one operator: a Helm chart (Chart) or a release manifest
// (Manifest), the CRD that shows it is installed, and its Deployments to wait
// for, in Namespace.
type Operator struct {
	Name        string
	Namespace   string
	Chart       *cluster.HelmRelease
	Manifest    string
	CRD         string
	Deployments []string
}

// Operators are operators, in install order.
type Operators []Operator
