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
	// Apply the ServiceAccount, RBAC, Services, and TLS Secret via SSA. These
	// must exist before the Deployment is built/applied below.
	for _, desired := range supportingResources(instance) {
		if err := r.applyResource(ctx, instance, desired); err != nil {
			return err
		}
	}
	// Read back the live ServiceAccount to get its server-assigned UID.
	sa := &corev1.ServiceAccount{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: operandNamespace, Name: operandName}, sa); err != nil {
		return common.FromClientError(err, "failed to read approver-policy ServiceAccount")
	}
	// Build the desired Deployment.
	deployment, err := getDeploymentObject(instance, sa.UID)
	if err != nil {
		return err
	}
	// Apply the Deployment via SSA.
	if err := r.applyResource(ctx, instance, deployment); err != nil {
		return err
	}
	// Apply the ValidatingWebhookConfiguration via SSA.
	if err := r.applyResource(ctx, instance, getWebhookObject(instance)); err != nil {
		return err
	}
	// The cache may lag behind apply. Keep reconciling until it reflects the
	// desired Deployment and that rollout is ready.
	observed := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(deployment), observed); err != nil {
		return common.FromClientError(err, "failed to observe approver-policy Deployment")
	}
	// Record the desired (not observed) image so status reflects intent even
	// while the rollout is still in progress.
	instance.Status.ApproverPolicyImage = deployment.Spec.Template.Spec.Containers[0].Image
	// Retry until the cache reflects what was applied and that rollout has
	// finished successfully; only then is reconciliation considered complete.
	if deploymentModified(deployment, observed) || !deploymentReady(observed) {
		return common.NewRetryRequiredError(fmt.Errorf("Deployment %s/%s is not available at the desired generation", operandNamespace, operandName), "waiting for approver-policy rollout")
	}
	return nil
}

// supportingResources returns every managed resource except the Deployment and
// webhook, which require the ServiceAccount to exist first.
func supportingResources(instance *v1alpha1.ApproverPolicyManager) []client.Object {
	resources := []client.Object{getServiceAccountObject(instance)}
	resources = append(resources, getRBACObjects(instance)...)
	resources = append(resources, getServiceObjects(instance)...)
	return append(resources, getSecretObject(instance))
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
func resourceModified(desired, existing client.Object) bool {
	switch desired := desired.(type) {
	case *corev1.ServiceAccount:
		return serviceAccountModified(desired, existing.(*corev1.ServiceAccount))
	case *corev1.Service:
		return serviceModified(desired, existing.(*corev1.Service))
	case *corev1.Secret:
		return managedMetadataModified(desired, existing)
	case *appsv1.Deployment:
		return deploymentModified(desired, existing.(*appsv1.Deployment))
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		return webhookModified(desired, existing.(*admissionregistrationv1.ValidatingWebhookConfiguration))
	case *rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding, *rbacv1.Role, *rbacv1.RoleBinding:
		return rbacModified(desired, existing)
	default:
		return true
	}
}
