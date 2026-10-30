package approverpolicy

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// cleanUp waits for workload garbage collection before removing its credentials
// and supporting resources. A successful DELETE is not proof of disappearance.
func (r *Reconciler) cleanUp(ctx context.Context, instance *v1alpha1.ApproverPolicyManager) (bool, error) {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: operandName, Namespace: operandNamespace}}
	pending, err := r.deleteResource(ctx, instance, deployment)
	if pending || err != nil {
		return pending, err
	}
	resources := append([]client.Object{getWebhookObject(instance)}, supportingResources(instance)...)
	var errs []error
	for _, obj := range resources {
		wait, err := r.deleteResource(ctx, instance, obj)
		pending = pending || wait
		errs = append(errs, err)
	}
	return pending, errors.Join(errs...)
}

func (r *Reconciler) deleteResource(ctx context.Context, instance *v1alpha1.ApproverPolicyManager, obj client.Object) (bool, error) {
	// Confirm existence directly: a cache miss may only mean the managed label
	// was removed, which must not allow cleanup to release the finalizer early.
	var err error
	if r.apiReader != nil {
		err = r.apiReader.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	} else {
		err = r.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	}
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return true, fmt.Errorf("failed to read %T %s during cleanup: %w", obj, client.ObjectKeyFromObject(obj), err)
	}
	if !metav1.IsControlledBy(obj, instance) {
		// A foreign replacement is not an orphan created by this CR.
		r.log.V(1).Info("leaving resource owned by another instance", "name", client.ObjectKeyFromObject(obj))
		return false, nil
	}
	if !obj.GetDeletionTimestamp().IsZero() {
		return true, nil
	}
	uid := obj.GetUID()
	if err := r.Delete(ctx, obj, client.Preconditions{UID: &uid}, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return true, fmt.Errorf("failed to delete %T %s: %w", obj, client.ObjectKeyFromObject(obj), err)
	}
	r.recordEvent(instance, "Deleted", "Requested deletion of %T %s", obj, client.ObjectKeyFromObject(obj))
	return true, nil
}
