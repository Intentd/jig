package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/intentd/jig/pkg/kubectl"
	"github.com/intentd/tools/cluster"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// preserveKubectlContext returns a func that switches kubectl back to the
// current context, or nil when there is none.
func preserveKubectlContext(ctx context.Context) func() {
	orig, err := kubectl.CurrentContext(ctx)
	if err != nil || orig == "" {
		return nil
	}
	return func() {
		if out, err := kubectl.UseContext(ctx, orig); err != nil {
			fmt.Printf("  ! Could not restore kubectl context to %q: %v\n%s\n", orig, err, out)
		} else {
			fmt.Printf("\n→ kubectl context restored to %q\n", orig)
		}
	}
}

// buildClient builds the Kubernetes clients from the active kubeconfig, once
// ensureCluster has selected the context.
func (r *runner) buildClient() error {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return err
	}
	r.cfg, r.cs, r.dyn = cfg, cs, dyn
	r.mapper = restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))
	return nil
}

// ensureCluster selects the context, or creates the kind cluster: recreated
// for a clean state unless environment.cluster.reuse.
func (r *runner) ensureCluster(ctx context.Context) error {
	switch {
	case r.o.UseCurrent:
		fmt.Printf("→ Using current cluster context...\n")
		return nil
	case r.o.Context != "":
		fmt.Printf("→ Using existing cluster context %q...\n", r.o.Context)
		if out, err := kubectl.UseContext(ctx, r.o.Context); err != nil {
			return fmt.Errorf("switching context: %w\n%s", err, out)
		}
		return nil
	}
	name := r.clusterName()
	if !r.spec.Environment.Reuse() && cluster.KindExists(ctx, name) {
		fmt.Printf("→ Recreating cluster '%s' (reuse: false)...\n", name)
		if err := r.deleteCluster(ctx); err != nil {
			return fmt.Errorf("deleting old cluster: %w", err)
		}
	}
	return cluster.EnsureKind(ctx, name, r.kindOptions())
}

func (r *runner) kindOptions() cluster.KindOptions {
	return cluster.KindOptions{Workers: r.o.Workers, Version: r.o.KindVersion, BinDir: r.o.KindBinDir}
}

func (r *runner) deleteCluster(ctx context.Context) error {
	return cluster.DeleteKind(ctx, r.clusterName(), cluster.KindOptions{BinDir: r.o.KindBinDir})
}

func (r *runner) clusterName() string {
	return r.spec.Environment.ClusterName(r.o.DefaultClusterName)
}

// effectiveContext is the context the run is on: --context, the kind
// context of a cluster it creates, else the current one.
func (r *runner) effectiveContext(ctx context.Context) string {
	if r.o.Context != "" {
		return r.o.Context
	}
	if r.needsCluster && !r.o.UseCurrent {
		return "kind-" + r.clusterName()
	}
	if c, err := kubectl.CurrentContext(ctx); err == nil {
		return c
	}
	return ""
}

// kindClusterName is the kind cluster the run is on, if it is one.
func (r *runner) kindClusterName(ctx context.Context) (string, bool) {
	c := r.effectiveContext(ctx)
	if !strings.HasPrefix(c, "kind-") {
		return "", false
	}
	return strings.TrimPrefix(c, "kind-"), true
}
