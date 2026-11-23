package ecosystem

import (
	"context"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/intentd/jig/pkg/ecosystem/operator"
	"github.com/intentd/tools/cluster"
	"github.com/intentd/tools/command"
	"github.com/intentd/tools/term"
)

// The data-services chart jig installs by default, pinned per jig release.
const (
	DefaultChart        = "oci://ghcr.io/rollwright/charts/data-services"
	DefaultChartVersion = "0.4.0"
	chartRelease        = "data-services"
)

// readyTimeout is how long an operator or an instance has to become ready:
// an operator pulls its image, an instance its own and sets up storage.
const readyTimeout = "10m"

// InstallOptions are where the data-services chart comes from.
type InstallOptions struct {
	// Chart is an oci:// reference or a local chart directory (for chart
	// work); DefaultChart when empty.
	Chart string
	// Version pins an oci:// chart; DefaultChartVersion with DefaultChart,
	// ignored for a local directory.
	Version string
}

// Install installs what the plan declares, in order: the operators the
// services need (one already running is used as it is, and left alone),
// the Secrets (credentials, then export and define), the data-services
// chart with the plan's values, then each instance's readiness. It returns
// what it did, in order, for the caller to undo in reverse, including what
// it did before an error.
func (p *Plan) Install(ctx context.Context, cs kubernetes.Interface, o InstallOptions) ([]Undo, error) {
	if p == nil {
		return nil, nil
	}
	var done []Undo
	for _, op := range p.Operators {
		u, err := installOperator(ctx, cs, op)
		if err != nil {
			return done, fmt.Errorf("operator %s: %w", op.Name, err)
		}
		done = append(done, u...)
	}

	var secrets []Undo
	n := len(p.Credentials) + len(p.Outputs)
	err := step(fmt.Sprintf("Writing %d Secret(s)...", n), fmt.Sprintf("%d Secret(s) written", n), func() error {
		var err error
		secrets, err = p.WriteSecrets(ctx, cs)
		return err
	})
	done = append(done, secrets...)
	if err != nil {
		return done, err
	}

	release, err := p.chartRelease(o)
	if err != nil {
		return done, err
	}
	err = step(fmt.Sprintf("Installing data-services (%s)...", release.Chart), "data-services installed", func() error {
		return cluster.HelmInstall(ctx, release)
	})
	os.Remove(release.ValueFiles[0])
	if err != nil {
		return done, err
	}
	done = append(done, Undo{Label: "release data-services", Do: func(ctx context.Context) error {
		return cluster.HelmUninstall(ctx, release)
	}})

	for _, w := range p.Ready {
		what := fmt.Sprintf("%s %s/%s", w.Kind, w.Namespace, w.Name)
		if err := step(fmt.Sprintf("Waiting for %s (%s)...", what, w.Condition), what+" ready", func() error {
			return cluster.WaitForResource(ctx, cluster.Wait{
				Kind: w.Kind, Name: w.Name, Namespace: w.Namespace, Ready: true, Condition: w.Condition, Timeout: readyTimeout,
			})
		}); err != nil {
			return done, err
		}
	}
	return done, nil
}

// chartRelease is the data-services release, its values written to a file.
func (p *Plan) chartRelease(o InstallOptions) (cluster.HelmRelease, error) {
	chart, version := o.Chart, o.Version
	if chart == "" {
		chart, version = DefaultChart, DefaultChartVersion
	}
	rel := cluster.HelmRelease{Chart: chart, Version: version, Release: chartRelease, Namespace: p.namespace(), CreateNamespace: true}
	if rel.IsLocalChart() {
		rel.Version = ""
	}
	values, err := yaml.Marshal(p.Values)
	if err != nil {
		return rel, err
	}
	f, err := os.CreateTemp("", "jig-data-services-*.yaml")
	if err != nil {
		return rel, err
	}
	defer f.Close()
	if _, err := f.Write(values); err != nil {
		return rel, err
	}
	rel.ValueFiles = []string{f.Name()}
	return rel, nil
}

// namespace is where the instances live: the first of the plan's
// namespaces an instance waits in, else the default.
func (p *Plan) namespace() string {
	for _, w := range p.Ready {
		return w.Namespace
	}
	return DefaultNamespace
}

// installOperator installs op unless it is already running, then waits for
// its Deployments; what it installed is undone with the run.
func installOperator(ctx context.Context, cs kubernetes.Interface, op operator.Operator) ([]Undo, error) {
	present, err := operatorPresent(ctx, cs, op)
	if err != nil {
		return nil, err
	}
	if present {
		fmt.Printf("  %s %s already running, used as it is\n", term.SuccessMark(), op.Name)
		return nil, nil
	}
	var done []Undo
	err = step(fmt.Sprintf("Installing %s...", op.Name), op.Name+" installed and ready", func() error {
		u, err := applyOperator(ctx, op)
		done = u
		return err
	})
	return done, err
}

// applyOperator installs op's chart or manifest, then waits for its
// Deployments.
func applyOperator(ctx context.Context, op operator.Operator) ([]Undo, error) {
	var done []Undo
	switch {
	case op.Chart != nil:
		rel := *op.Chart
		if err := cluster.HelmInstall(ctx, rel); err != nil {
			return nil, err
		}
		done = append(done, Undo{Label: "operator " + op.Name, Do: func(ctx context.Context) error {
			return cluster.HelmUninstall(ctx, rel)
		}})
	case op.Manifest != "":
		if out, err := kubectl(ctx, "apply", "--server-side", "-f", op.Manifest); err != nil {
			return nil, fmt.Errorf("%w\n%s", err, out)
		}
		manifest := op.Manifest
		done = append(done, Undo{Label: "operator " + op.Name, Do: func(ctx context.Context) error {
			_, err := kubectl(ctx, "delete", "-f", manifest, "--ignore-not-found", "--wait=false")
			return err
		}})
	}
	for _, d := range op.Deployments {
		if err := cluster.WaitForResource(ctx, cluster.Wait{Kind: "Deployment", Name: d, Namespace: op.Namespace, Ready: true, Timeout: readyTimeout}); err != nil {
			return done, err
		}
	}
	return done, nil
}

// operatorPresent reports whether op runs already: its CRD is in the
// cluster, or, for one without a CRD of its own, its first Deployment is.
func operatorPresent(ctx context.Context, cs kubernetes.Interface, op operator.Operator) (bool, error) {
	if op.CRD != "" {
		out, err := kubectl(ctx, "get", "crd", op.CRD, "--ignore-not-found", "-o", "name")
		if err != nil {
			return false, fmt.Errorf("looking for CRD %s: %w", op.CRD, err)
		}
		return strings.TrimSpace(out) != "", nil
	}
	if len(op.Deployments) == 0 {
		return false, nil
	}
	_, err := cs.AppsV1().Deployments(op.Namespace).Get(ctx, op.Deployments[0], metav1.GetOptions{})
	switch {
	case err == nil:
		return true, nil
	case apierrors.IsNotFound(err):
		return false, nil
	}
	return false, err
}

// kubectl runs kubectl with args, returning its combined output.
func kubectl(ctx context.Context, args ...string) (string, error) {
	r, err := command.Run(ctx, command.Args("kubectl", args...))
	if err == nil {
		err = r.Err()
	}
	return r.Output, err
}

// step runs fn under a spinner showing msg, then marks it: ok on success,
// msg failed otherwise.
func step(msg, ok string, fn func() error) error {
	sp := term.StartSpinner(msg)
	if err := fn(); err != nil {
		sp.FailureWithUpdate(strings.TrimSuffix(msg, "...") + " failed")
		return err
	}
	sp.SuccessWithUpdate(ok)
	return nil
}
