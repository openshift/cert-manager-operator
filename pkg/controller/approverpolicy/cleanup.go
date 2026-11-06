package approverpolicy

import (
	"context"
	"errors"
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// cleanUp follows Helm's uninstall order for the managed resource kinds:
// https://github.com/helm/helm/blob/main/internal/release/v2/util/kind_sorter.go
// Wait for webhook removal before its backend, and workload garbage collection
// before RBAC and credentials. A successful DELETE is not proof of disappearance.
func (r *Reconciler) cleanUp(ctx context.Context, instance *v1alpha1.ApproverPolicyManager) (bool, error) {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: operandName, Namespace: operandNamespace}}
	rbacResources := getRBACObjects(instance)
	// Reverse installation order: RoleBinding, Role, ClusterRoleBinding, ClusterRole.
	slices.Reverse(rbacResources)
	phases := [][]client.Object{
		{getWebhookObject(instance)},
		append(getServiceObjects(instance), deployment),
		append(rbacResources, getSecretObject(instance), getServiceAccountObject(instance)),
	}
	for _, resources := range phases {
		var pending bool
		var errs []error
		for _, obj := range resources {
			wait, err := r.deleteResource(ctx, instance, obj)
			pending = pending || wait
			errs = append(errs, err)
		}
		if err := errors.Join(errs...); pending || err != nil {
			return pending, err
		}
	}
	return false, nil
}

func (r *Reconciler) deleteResource(ctx context.Context, instance *v1alpha1.ApproverPolicyManager, obj client.Object) (bool, error) {
	// Confirm via the API: resources can fall out of the filtered cache when
	// their managed label is removed, without actually being deleted.
	if err := r.apiReader.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
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
