package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyRBACResources(tm *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}

	// ClusterRole
	if err := r.createOrApplyClusterRole(tm, clusterRoleAssetName, resourceLabels, trustManagerCreateRecon); err != nil {
		return err
	}

	// ClusterRoleBinding
	if err := r.createOrApplyClusterRoleBinding(tm, clusterRoleBindingAssetName, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		return err
	}

	// Namespace-scoped Role
	if err := r.createOrApplyRole(tm, roleAssetName, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		return err
	}

	// Namespace-scoped RoleBinding
	if err := r.createOrApplyRoleBinding(tm, roleBindingAssetName, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		return err
	}

	// Leases Role
	if err := r.createOrApplyRole(tm, roleLeasesAssetName, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		return err
	}

	// Leases RoleBinding
	if err := r.createOrApplyRoleBinding(tm, roleBindingLeasesAssetName, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		return err
	}

	// Secret targets RBAC: create or delete based on policy
	if err := r.reconcileSecretTargetsRBAC(tm, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		return err
	}

	return nil
}

func (r *Reconciler) reconcileSecretTargetsRBAC(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	policy := tm.Spec.TrustManagerConfig.SecretTargets.Policy
	if policy == v1alpha1.SecretTargetsPolicyCustom {
		// Create the secret targets ClusterRole and ClusterRoleBinding
		if err := r.createOrApplyClusterRole(tm, secretTargetsClusterRoleAssetName, resourceLabels, trustManagerCreateRecon); err != nil {
			return err
		}
		if err := r.createOrApplyClusterRoleBinding(tm, secretTargetsClusterRoleBindingAssetName, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
			return err
		}
	} else {
		// Delete the secret targets ClusterRole and ClusterRoleBinding if they exist
		if err := r.deleteSecretTargetsRBACIfExists(); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) deleteSecretTargetsRBACIfExists() error {
	// Try to delete secret targets ClusterRole
	clusterRole := &rbacv1.ClusterRole{}
	crKey := client.ObjectKey{Name: "trust-manager-secret-targets"}
	exist, err := r.Exists(r.ctx, crKey, clusterRole)
	if err != nil {
		return FromClientError(err, "failed to check if secret targets clusterrole exists")
	}
	if exist {
		if err := r.Delete(r.ctx, clusterRole); err != nil {
			return FromClientError(err, "failed to delete secret targets clusterrole")
		}
		r.log.V(1).Info("deleted secret targets clusterrole", "name", crKey.Name)
	}

	// Try to delete secret targets ClusterRoleBinding
	crb := &rbacv1.ClusterRoleBinding{}
	crbKey := client.ObjectKey{Name: "trust-manager-secret-targets"}
	exist, err = r.Exists(r.ctx, crbKey, crb)
	if err != nil {
		return FromClientError(err, "failed to check if secret targets clusterrolebinding exists")
	}
	if exist {
		if err := r.Delete(r.ctx, crb); err != nil {
			return FromClientError(err, "failed to delete secret targets clusterrolebinding")
		}
		r.log.V(1).Info("deleted secret targets clusterrolebinding", "name", crbKey.Name)
	}

	return nil
}

func (r *Reconciler) createOrApplyClusterRole(tm *v1alpha1.TrustManager, assetName string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	clusterRole := decodeClusterRoleObjBytes(assets.MustAsset(assetName))
	updateResourceLabels(clusterRole, resourceLabels)

	resourceName := clusterRole.GetName()
	r.log.V(4).Info("reconciling clusterrole resource", "name", resourceName)

	fetched := &rbacv1.ClusterRole{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(clusterRole), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s clusterrole resource already exists", resourceName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrole resource already exists, maybe from previous installation", resourceName)
	}
	if exist && hasObjectChanged(clusterRole, fetched) {
		r.log.V(1).Info("clusterrole has been modified, updating to desired state", "name", resourceName)
		if err := r.UpdateWithRetry(r.ctx, clusterRole); err != nil {
			return FromClientError(err, "failed to update %s clusterrole resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s reconciled back to desired state", resourceName)
	} else {
		r.log.V(4).Info("clusterrole resource already exists and is in expected state", "name", resourceName)
	}
	if !exist {
		if err := r.Create(r.ctx, clusterRole); err != nil {
			return FromClientError(err, "failed to create %s clusterrole resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s created", resourceName)
	}

	return nil
}

func (r *Reconciler) createOrApplyClusterRoleBinding(tm *v1alpha1.TrustManager, assetName string, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	crb := decodeClusterRoleBindingObjBytes(assets.MustAsset(assetName))
	updateResourceLabels(crb, resourceLabels)
	// Update subject namespace to the trust namespace
	for i := range crb.Subjects {
		if crb.Subjects[i].Kind == "ServiceAccount" {
			crb.Subjects[i].Namespace = trustNamespace
		}
	}

	resourceName := crb.GetName()
	r.log.V(4).Info("reconciling clusterrolebinding resource", "name", resourceName)

	fetched := &rbacv1.ClusterRoleBinding{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(crb), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s clusterrolebinding resource already exists", resourceName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrolebinding resource already exists, maybe from previous installation", resourceName)
	}
	if exist && hasObjectChanged(crb, fetched) {
		r.log.V(1).Info("clusterrolebinding has been modified, updating to desired state", "name", resourceName)
		if err := r.UpdateWithRetry(r.ctx, crb); err != nil {
			return FromClientError(err, "failed to update %s clusterrolebinding resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s reconciled back to desired state", resourceName)
	} else {
		r.log.V(4).Info("clusterrolebinding resource already exists and is in expected state", "name", resourceName)
	}
	if !exist {
		if err := r.Create(r.ctx, crb); err != nil {
			return FromClientError(err, "failed to create %s clusterrolebinding resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s created", resourceName)
	}

	return nil
}

func (r *Reconciler) createOrApplyRole(tm *v1alpha1.TrustManager, assetName string, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	role := decodeRoleObjBytes(assets.MustAsset(assetName))
	updateNamespace(role, trustNamespace)
	updateResourceLabels(role, resourceLabels)

	resourceName := fmt.Sprintf("%s/%s", role.GetNamespace(), role.GetName())
	r.log.V(4).Info("reconciling role resource", "name", resourceName)

	fetched := &rbacv1.Role{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(role), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s role resource already exists", resourceName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s role resource already exists, maybe from previous installation", resourceName)
	}
	if exist && hasObjectChanged(role, fetched) {
		r.log.V(1).Info("role has been modified, updating to desired state", "name", resourceName)
		if err := r.UpdateWithRetry(r.ctx, role); err != nil {
			return FromClientError(err, "failed to update %s role resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "role resource %s reconciled back to desired state", resourceName)
	} else {
		r.log.V(4).Info("role resource already exists and is in expected state", "name", resourceName)
	}
	if !exist {
		if err := r.Create(r.ctx, role); err != nil {
			return FromClientError(err, "failed to create %s role resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "role resource %s created", resourceName)
	}

	return nil
}

func (r *Reconciler) createOrApplyRoleBinding(tm *v1alpha1.TrustManager, assetName string, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	rb := decodeRoleBindingObjBytes(assets.MustAsset(assetName))
	updateNamespace(rb, trustNamespace)
	updateResourceLabels(rb, resourceLabels)
	// Update subject namespace
	for i := range rb.Subjects {
		if rb.Subjects[i].Kind == "ServiceAccount" {
			rb.Subjects[i].Namespace = trustNamespace
		}
	}

	resourceName := fmt.Sprintf("%s/%s", rb.GetNamespace(), rb.GetName())
	r.log.V(4).Info("reconciling rolebinding resource", "name", resourceName)

	fetched := &rbacv1.RoleBinding{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(rb), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s rolebinding resource already exists", resourceName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s rolebinding resource already exists, maybe from previous installation", resourceName)
	}
	if exist && hasObjectChanged(rb, fetched) {
		r.log.V(1).Info("rolebinding has been modified, updating to desired state", "name", resourceName)
		if err := r.UpdateWithRetry(r.ctx, rb); err != nil {
			return FromClientError(err, "failed to update %s rolebinding resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s reconciled back to desired state", resourceName)
	} else {
		r.log.V(4).Info("rolebinding resource already exists and is in expected state", "name", resourceName)
	}
	if !exist {
		if err := r.Create(r.ctx, rb); err != nil {
			return FromClientError(err, "failed to create %s rolebinding resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s created", resourceName)
	}

	return nil
}
