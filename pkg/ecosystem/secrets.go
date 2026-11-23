package ecosystem

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/intentd/tools/kube"
)

// Undo is one thing WriteSecrets did, and how to undo it.
type Undo struct {
	// Label names it, such as "Secret shop/shop-deps".
	Label string
	Do    func(context.Context) error
}

// WriteSecrets writes the plan's Secrets: the credentials the chart's
// entries name, then the export and define Secrets, each created or its
// keys updated. A namespace they or the instances need is created when it
// is missing. It returns what it did, in order, for the caller to undo in
// reverse: a namespace only when it created it, so one that was there stays.
func (p *Plan) WriteSecrets(ctx context.Context, cs kubernetes.Interface) ([]Undo, error) {
	if p == nil {
		return nil, nil
	}
	var done []Undo
	for _, ns := range p.Namespaces {
		created, err := ensureNamespace(ctx, cs, ns)
		if err != nil {
			return done, err
		}
		if created {
			done = append(done, Undo{Label: "namespace " + ns, Do: func(ctx context.Context) error {
				err := cs.CoreV1().Namespaces().Delete(ctx, ns, metav1.DeleteOptions{})
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}})
		}
	}
	for _, sec := range append(append(Secrets{}, p.Credentials...), p.Outputs...) {
		opts := []kube.SecretOption{kube.WithSecretLabels(sec.Labels)}
		if sec.Type != "" {
			opts = append(opts, kube.WithSecretType(corev1.SecretType(sec.Type)))
		}
		if err := kube.WriteSecretKeys(ctx, cs, sec.Namespace, sec.Name, sec.Data, opts...); err != nil {
			return done, fmt.Errorf("Secret %s/%s: %w", sec.Namespace, sec.Name, err)
		}
		done = append(done, Undo{Label: "Secret " + sec.Namespace + "/" + sec.Name, Do: func(ctx context.Context) error {
			return kube.DeleteSecret(ctx, cs, sec.Namespace, sec.Name)
		}})
	}
	return done, nil
}

// ensureNamespace creates ns when it is missing, and reports whether it did.
func ensureNamespace(ctx context.Context, cs kubernetes.Interface, ns string) (bool, error) {
	_, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{})
	switch {
	case err == nil:
		return true, nil
	case apierrors.IsAlreadyExists(err):
		return false, nil
	}
	return false, fmt.Errorf("namespace %s: %w", ns, err)
}
