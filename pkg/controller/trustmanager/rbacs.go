package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyRBACResource(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	serviceAccount := decodeServiceAccountObjBytes(assets.MustAsset(serviceAccountAssetName)).GetName()

	if err := r.createOrApplyClusterRoles(tm, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile clusterrole resource")
		return err
	}

	if err := r.createOrApplyClusterRoleBindings(tm, serviceAccount, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile clusterrolebinding resource")
		return err
	}

	if err := r.createOrApplyRoleForLeases(tm, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile role for leases resource")
		return err
	}

	if err := r.createOrApplyRoleBindingForLeases(tm, serviceAccount, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile rolebinding for leases resource")
		return err
	}

	// Handle SecretTargets RBAC: create or clean up secret targets ClusterRole/ClusterRoleBinding
	if tm.Spec.TrustManagerConfig.SecretTargets.Policy == v1alpha1.SecretTargetsPolicyCustom {
		if err := r.createOrApplySecretTargetsRBAC(tm, serviceAccount, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
			r.log.Error(err, "failed to reconcile secret targets rbac resources")
			return err
		}
	} else {
		if err := r.cleanUpSecretTargetsRBAC(tm); err != nil {
			r.log.Error(err, "failed to clean up secret targets rbac resources")
			return err
		}
	}

	return nil
}

func (r *Reconciler) createOrApplyClusterRoles(tm *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getClusterRoleObject(resourceLabels)

	roleName := desired.GetName()
	r.log.V(4).Info("reconciling clusterrole resource", "name", roleName)
	fetched := &rbacv1.ClusterRole{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s clusterrole resource already exists", roleName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrole resource already exists, maybe from previous installation", roleName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("clusterrole has been modified, updating to desired state", "name", roleName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s reconciled back to desired state", roleName)
	} else {
		r.log.V(4).Info("clusterrole resource already exists and is in expected state", "name", roleName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s created", roleName)
	}

	return nil
}

func (r *Reconciler) getClusterRoleObject(resourceLabels map[string]string) *rbacv1.ClusterRole {
	clusterRole := decodeClusterRoleObjBytes(assets.MustAsset(clusterRoleAssetName))
	updateResourceLabels(clusterRole, resourceLabels)
	return clusterRole
}

func (r *Reconciler) createOrApplyClusterRoleBindings(tm *v1alpha1.TrustManager, serviceAccount, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getClusterRoleBindingObject(serviceAccount, trustNamespace, resourceLabels)

	roleBindingName := desired.GetName()
	r.log.V(4).Info("reconciling clusterrolebinding resource", "name", roleBindingName)
	fetched := &rbacv1.ClusterRoleBinding{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s clusterrolebinding resource already exists", roleBindingName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrolebinding resource already exists, maybe from previous installation", roleBindingName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("clusterrolebinding has been modified, updating to desired state", "name", roleBindingName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s clusterrolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s reconciled back to desired state", roleBindingName)
	} else {
		r.log.V(4).Info("clusterrolebinding resource already exists and is in expected state", "name", roleBindingName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s clusterrolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s created", roleBindingName)
	}

	return nil
}

func (r *Reconciler) getClusterRoleBindingObject(serviceAccount, trustNamespace string, resourceLabels map[string]string) *rbacv1.ClusterRoleBinding {
	clusterRoleBinding := decodeClusterRoleBindingObjBytes(assets.MustAsset(clusterRoleBindingAssetName))
	updateResourceLabels(clusterRoleBinding, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.ClusterRoleBinding](clusterRoleBinding, serviceAccount, trustNamespace)
	return clusterRoleBinding
}

func (r *Reconciler) createOrApplyRoleForLeases(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getRoleForLeasesObject(trustNamespace, resourceLabels)

	roleName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling role for lease resource", "name", roleName)
	fetched := &rbacv1.Role{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s role resource already exists", roleName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s role resource already exists, maybe from previous installation", roleName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("role has been modified, updating to desired state", "name", roleName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s role resource", roleName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "role resource %s reconciled back to desired state", roleName)
	} else {
		r.log.V(4).Info("role resource already exists and is in expected state", "name", roleName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s role resource", roleName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "role resource %s created", roleName)
	}

	return nil
}

func (r *Reconciler) getRoleForLeasesObject(roleNamespace string, resourceLabels map[string]string) *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleLeasesAssetName))
	updateNamespace(role, roleNamespace)
	updateResourceLabels(role, resourceLabels)
	return role
}

func (r *Reconciler) createOrApplyRoleBindingForLeases(tm *v1alpha1.TrustManager, serviceAccount, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getRoleBindingForLeasesObject(serviceAccount, trustNamespace, resourceLabels)

	roleBindingName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling rolebinding for lease resource", "name", roleBindingName)
	fetched := &rbacv1.RoleBinding{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s rolebinding resource already exists", roleBindingName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s rolebinding resource already exists, maybe from previous installation", roleBindingName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("rolebinding has been modified, updating to desired state", "name", roleBindingName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s rolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s reconciled back to desired state", roleBindingName)
	} else {
		r.log.V(4).Info("rolebinding resource already exists and is in expected state", "name", roleBindingName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s rolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s created", roleBindingName)
	}

	return nil
}

func (r *Reconciler) getRoleBindingForLeasesObject(serviceAccount, trustNamespace string, resourceLabels map[string]string) *rbacv1.RoleBinding {
	roleBinding := decodeRoleBindingObjBytes(assets.MustAsset(roleBindingLeasesAssetName))
	updateNamespace(roleBinding, trustNamespace)
	updateResourceLabels(roleBinding, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.RoleBinding](roleBinding, serviceAccount, trustNamespace)
	return roleBinding
}

func (r *Reconciler) createOrApplySecretTargetsRBAC(tm *v1alpha1.TrustManager, serviceAccount, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	// Create the secret targets ClusterRole
	desiredRole := decodeClusterRoleObjBytes(assets.MustAsset(secretTargetsRoleAssetName))
	updateResourceLabels(desiredRole, resourceLabels)

	roleName := desiredRole.GetName()
	r.log.V(4).Info("reconciling secret targets clusterrole resource", "name", roleName)
	fetchedRole := &rbacv1.ClusterRole{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desiredRole), fetchedRole)
	if err != nil {
		return FromClientError(err, "failed to check %s clusterrole resource already exists", roleName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrole resource already exists, maybe from previous installation", roleName)
	}
	if exist && hasObjectChanged(desiredRole, fetchedRole) {
		r.log.V(1).Info("secret targets clusterrole has been modified, updating to desired state", "name", roleName)
		if err := r.UpdateWithRetry(r.ctx, desiredRole); err != nil {
			return FromClientError(err, "failed to update %s clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s reconciled back to desired state", roleName)
	}
	if !exist {
		if err := r.Create(r.ctx, desiredRole); err != nil {
			return FromClientError(err, "failed to create %s clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s created", roleName)
	}

	// Create the secret targets ClusterRoleBinding
	desiredBinding := decodeClusterRoleBindingObjBytes(assets.MustAsset(secretTargetsRoleBindingName))
	updateResourceLabels(desiredBinding, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.ClusterRoleBinding](desiredBinding, serviceAccount, trustNamespace)

	bindingName := desiredBinding.GetName()
	r.log.V(4).Info("reconciling secret targets clusterrolebinding resource", "name", bindingName)
	fetchedBinding := &rbacv1.ClusterRoleBinding{}
	exist, err = r.Exists(r.ctx, client.ObjectKeyFromObject(desiredBinding), fetchedBinding)
	if err != nil {
		return FromClientError(err, "failed to check %s clusterrolebinding resource already exists", bindingName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrolebinding resource already exists, maybe from previous installation", bindingName)
	}
	if exist && hasObjectChanged(desiredBinding, fetchedBinding) {
		r.log.V(1).Info("secret targets clusterrolebinding has been modified, updating to desired state", "name", bindingName)
		if err := r.UpdateWithRetry(r.ctx, desiredBinding); err != nil {
			return FromClientError(err, "failed to update %s clusterrolebinding resource", bindingName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s reconciled back to desired state", bindingName)
	}
	if !exist {
		if err := r.Create(r.ctx, desiredBinding); err != nil {
			return FromClientError(err, "failed to create %s clusterrolebinding resource", bindingName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s created", bindingName)
	}

	return nil
}

// cleanUpSecretTargetsRBAC removes secret targets ClusterRole and ClusterRoleBinding
// when the SecretTargets policy is changed from Custom to Disabled.
func (r *Reconciler) cleanUpSecretTargetsRBAC(tm *v1alpha1.TrustManager) error {
	secretTargetsRole := decodeClusterRoleObjBytes(assets.MustAsset(secretTargetsRoleAssetName))
	fetchedRole := &rbacv1.ClusterRole{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(secretTargetsRole), fetchedRole)
	if err != nil {
		return FromClientError(err, "failed to check if secret targets clusterrole exists for cleanup")
	}
	if exist {
		if err := r.Delete(r.ctx, fetchedRole); err != nil {
			return FromClientError(err, "failed to delete secret targets clusterrole during cleanup")
		}
		r.log.V(1).Info("deleted secret targets clusterrole", "name", fetchedRole.GetName())
	}

	secretTargetsBinding := decodeClusterRoleBindingObjBytes(assets.MustAsset(secretTargetsRoleBindingName))
	fetchedBinding := &rbacv1.ClusterRoleBinding{}
	exist, err = r.Exists(r.ctx, client.ObjectKeyFromObject(secretTargetsBinding), fetchedBinding)
	if err != nil {
		return FromClientError(err, "failed to check if secret targets clusterrolebinding exists for cleanup")
	}
	if exist {
		if err := r.Delete(r.ctx, fetchedBinding); err != nil {
			return FromClientError(err, "failed to delete secret targets clusterrolebinding during cleanup")
		}
		r.log.V(1).Info("deleted secret targets clusterrolebinding", "name", fetchedBinding.GetName())
	}

	return nil
}
