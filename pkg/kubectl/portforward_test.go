package kubectl

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/intentd/jig/pkg/spec"
	"github.com/intentd/tools/assert"
	"github.com/intentd/tools/kubeclient/kubefake"
)

func TestAssertPortForwardOutput(t *testing.T) {
	e := spec.KubectlPortForward{Path: "/healthz", Assert: &assert.Assertions{TextAssertion: assert.TextAssertion{Contains: "ok"}}}
	if err := assertPortForwardOutput(context.Background(), "", `{"status":"ok"}`, e, "pod/inrun-runtime-0"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := assertPortForwardOutput(context.Background(), "", `{"status":"degraded"}`, e, "pod/inrun-runtime-0"); err == nil {
		t.Fatal("expected error — output does not contain \"ok\"")
	}
}

func TestAssertPortForwardOutput_StatusCode(t *testing.T) {
	e := spec.KubectlPortForward{Path: "/healthz", StatusCode: 200, Assert: &assert.Assertions{Equals: "200"}}
	if err := assertPortForwardOutput(context.Background(), "", "200", e, "pod/inrun-runtime-0"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if err := assertPortForwardOutput(context.Background(), "", "503", e, "pod/inrun-runtime-0"); err == nil {
		t.Fatal("expected error — status code 503 does not equal \"200\"")
	}
}

// A service port-forward picks a pod the service would send traffic to:
// not one still terminating after a rollout, not one that isn't ready.
func TestResolveServicePod_SkipsTerminatingAndUnready(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "greeter", Namespace: "greeter"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "greeter"}},
	}
	pod := func(name string, ready, deleting bool) *corev1.Pod {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "greeter", Labels: map[string]string{"app": "greeter"}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		}
		status := corev1.ConditionFalse
		if ready {
			status = corev1.ConditionTrue
		}
		p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}
		if deleting {
			now := metav1.Now()
			p.DeletionTimestamp = &now
			p.Finalizers = []string{"test/hold"}
		}
		return p
	}
	cs := kubefake.NewClientset(svc,
		pod("greeter-old", true, true),
		pod("greeter-starting", false, false),
		pod("greeter-new", true, false),
	)
	got, err := resolveServicePod(context.Background(), cs, "greeter", "greeter")
	if err != nil {
		t.Fatal(err)
	}
	if got != "greeter-new" {
		t.Fatalf("resolveServicePod = %q, want greeter-new", got)
	}
}
