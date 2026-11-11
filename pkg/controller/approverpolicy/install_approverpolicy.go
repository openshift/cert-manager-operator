package approverpolicy

import (
	"context"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func (r *Reconciler) reconcileApproverPolicy(ctx context.Context, instance *v1alpha1.ApproverPolicyManager) error {
	// Apply ServiceAccount via SSA.
	if err := r.createOrApplyServiceAccount(ctx, instance); err != nil {
		return err
	}
	// Apply TLS Secret via SSA.
	if err := r.createOrApplySecret(ctx, instance); err != nil {
		return err
	}
	// Apply RBAC objects via SSA.
	if err := r.createOrApplyRBACResources(ctx, instance); err != nil {
		return err
	}
	// Apply Services via SSA.
	if err := r.createOrApplyServices(ctx, instance); err != nil {
		return err
	}
	// Apply the Deployment via SSA and wait for its rollout.
	if err := r.createOrApplyDeployment(ctx, instance); err != nil {
		return err
	}
	// Register the webhook only after its backend Deployment is ready.
	if err := r.createOrApplyValidatingWebhookConfiguration(ctx, instance); err != nil {
		return err
	}
	return nil
}

// applyResource server-side applies desired if it is missing or has drifted from
// the live object, skipping the write otherwise.
func (r *Reconciler) applyResource(ctx context.Context, instance *v1alpha1.ApproverPolicyManager, desired client.Object) error {
	existing := desired.DeepCopyObject().(client.Object)
	key := client.ObjectKeyFromObject(desired)
	err := r.Get(ctx, key, existing)
	if apierrors.IsNotFound(err) && r.apiReader != nil {
		// An object without the managed label is absent from the filtered cache.
		// Check the API before deciding whether it is safe to create or apply.
		err = r.apiReader.Get(ctx, key, existing)
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return common.FromClientError(err, "failed to read %T %s", desired, client.ObjectKeyFromObject(desired))
	}
	if err == nil {
		if !metav1.IsControlledBy(existing, instance) {
			return common.NewIrrecoverableError(fmt.Errorf("%T %s is not owned by ApproverPolicyManager %s", existing, client.ObjectKeyFromObject(existing), instance.UID), "resource ownership conflict")
		}
		if !existing.GetDeletionTimestamp().IsZero() {
			return common.NewRetryRequiredError(fmt.Errorf("%T %s is terminating", existing, client.ObjectKeyFromObject(existing)), "waiting for managed resource deletion")
		}
		if !resourceModified(desired, existing) {
			return nil
		}
		// Prevent a concurrent delete/recreate from applying our intent to a
		// different object at this name.
		desired.SetUID(existing.GetUID())
		desired.SetResourceVersion(existing.GetResourceVersion())
	}
	r.log.V(2).Info("applying managed resource", "kind", desired.GetObjectKind().GroupVersionKind().Kind, "name", client.ObjectKeyFromObject(desired))
	if err := r.Patch(ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply %T %s", desired, client.ObjectKeyFromObject(desired))
	}
	r.recordEvent(instance, "Reconciled", "Applied %T %s", desired, client.ObjectKeyFromObject(desired))
	return nil
}

// resourceModified dispatches to the type-specific drift check applyResource needs
// to decide whether a write is required.
// applyResource creates existing by deep-copying desired, so their types match.
func resourceModified(desired, existing client.Object) bool {
	switch desired := desired.(type) {
	case *corev1.ServiceAccount:
		return common.ServiceAccountModified(desired, existing.(*corev1.ServiceAccount), fieldOwner)
	case *corev1.Service:
		return common.ServiceModified(desired, existing.(*corev1.Service), fieldOwner)
	case *corev1.Secret:
		return common.ManagedMetadataModified(desired, existing, fieldOwner)
	case *appsv1.Deployment:
		return deploymentModified(desired, existing.(*appsv1.Deployment))
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		return common.ValidatingWebhookConfigurationModified(desired, existing.(*admissionregistrationv1.ValidatingWebhookConfiguration), fieldOwner)
	case *rbacv1.ClusterRole:
		return common.ClusterRoleModified(desired, existing.(*rbacv1.ClusterRole), fieldOwner)
	case *rbacv1.ClusterRoleBinding:
		return common.ClusterRoleBindingModified(desired, existing.(*rbacv1.ClusterRoleBinding), fieldOwner)
	case *rbacv1.Role:
		return common.RoleModified(desired, existing.(*rbacv1.Role), fieldOwner)
	case *rbacv1.RoleBinding:
		return common.RoleBindingModified(desired, existing.(*rbacv1.RoleBinding), fieldOwner)
	default:
		return true
	}
}
