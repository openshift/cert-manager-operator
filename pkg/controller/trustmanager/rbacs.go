package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyRBACResources(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	serviceAccount := decodeServiceAccountObjBytes(assets.MustAsset(serviceAccountAssetName)).GetName()

	if err := r.createOrApplyClusterRoles(trustManager, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile clusterrole resource")
		return err
	}

	if err := r.createOrApplyClusterRoleBindings(trustManager, serviceAccount, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile clusterrolebinding resource")
		return err
	}

	if err := r.createOrApplyRoles(trustManager, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile role resource")
		return err
	}

	if err := r.createOrApplyRoleBindings(trustManager, serviceAccount, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile rolebinding resource")
		return err
	}

	// Handle dynamic RBAC for secret targets
	if trustManager.Spec.TrustManagerConfig.SecretTargets.Policy == v1alpha1.SecretTargetsPolicyCustom {
		if err := r.createOrApplySecretTargetsRBAC(trustManager, serviceAccount, resourceLabels, trustManagerCreateRecon); err != nil {
			r.log.Error(err, "failed to reconcile secret targets rbac resources")
			return err
		}
	}

	return nil
}

func (r *Reconciler) createOrApplyClusterRoles(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getClusterRoleObject(resourceLabels)

	roleName := desired.GetName()
	r.log.V(4).Info("reconciling clusterrole resource", "name", roleName)
	fetched := &rbacv1.ClusterRole{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s clusterrole resource already exists", roleName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrole resource already exists, maybe from previous installation", roleName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("clusterrole has been modified, updating to desired state", "name", roleName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s reconciled back to desired state", roleName)
	} else {
		r.log.V(4).Info("clusterrole resource already exists and is in expected state", "name", roleName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s created", roleName)
	}

	return nil
}

func (r *Reconciler) getClusterRoleObject(resourceLabels map[string]string) *rbacv1.ClusterRole {
	clusterRole := decodeClusterRoleObjBytes(assets.MustAsset(clusterRoleAssetName))
	updateResourceLabels(clusterRole, resourceLabels)
	return clusterRole
}

func (r *Reconciler) createOrApplyClusterRoleBindings(trustManager *v1alpha1.TrustManager, serviceAccount string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getClusterRoleBindingObject(serviceAccount, resourceLabels)

	roleBindingName := desired.GetName()
	r.log.V(4).Info("reconciling clusterrolebinding resource", "name", roleBindingName)
	fetched := &rbacv1.ClusterRoleBinding{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s clusterrolebinding resource already exists", roleBindingName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrolebinding resource already exists, maybe from previous installation", roleBindingName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("clusterrolebinding has been modified, updating to desired state", "name", roleBindingName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s clusterrolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s reconciled back to desired state", roleBindingName)
	} else {
		r.log.V(4).Info("clusterrolebinding resource already exists and is in expected state", "name", roleBindingName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s clusterrolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s created", roleBindingName)
	}

	return nil
}

func (r *Reconciler) getClusterRoleBindingObject(serviceAccount string, resourceLabels map[string]string) *rbacv1.ClusterRoleBinding {
	clusterRoleBinding := decodeClusterRoleBindingObjBytes(assets.MustAsset(clusterRoleBindingAssetName))
	updateResourceLabels(clusterRoleBinding, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.ClusterRoleBinding](clusterRoleBinding, serviceAccount, trustManagerOperandNamespace)
	return clusterRoleBinding
}

func (r *Reconciler) createOrApplyRoles(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getRoleObject(trustManager, resourceLabels)

	roleName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling role resource", "name", roleName)
	fetched := &rbacv1.Role{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s role resource already exists", roleName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s role resource already exists, maybe from previous installation", roleName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("role has been modified, updating to desired state", "name", roleName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s role resource", roleName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "role resource %s reconciled back to desired state", roleName)
	} else {
		r.log.V(4).Info("role resource already exists and is in expected state", "name", roleName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s role resource", roleName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "role resource %s created", roleName)
	}

	return nil
}

func (r *Reconciler) getRoleObject(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string) *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleAssetName))
	// Deploy the role in the trust namespace
	trustNamespace := trustManager.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = trustManagerOperandNamespace
	}
	updateNamespace(role, trustNamespace)
	updateResourceLabels(role, resourceLabels)
	return role
}

func (r *Reconciler) createOrApplyRoleBindings(trustManager *v1alpha1.TrustManager, serviceAccount string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getRoleBindingObject(trustManager, serviceAccount, resourceLabels)

	roleBindingName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling rolebinding resource", "name", roleBindingName)
	fetched := &rbacv1.RoleBinding{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s rolebinding resource already exists", roleBindingName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s rolebinding resource already exists, maybe from previous installation", roleBindingName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("rolebinding has been modified, updating to desired state", "name", roleBindingName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s rolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s reconciled back to desired state", roleBindingName)
	} else {
		r.log.V(4).Info("rolebinding resource already exists and is in expected state", "name", roleBindingName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s rolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s created", roleBindingName)
	}

	return nil
}

func (r *Reconciler) getRoleBindingObject(trustManager *v1alpha1.TrustManager, serviceAccount string, resourceLabels map[string]string) *rbacv1.RoleBinding {
	roleBinding := decodeRoleBindingObjBytes(assets.MustAsset(roleBindingAssetName))
	// Deploy the rolebinding in the trust namespace
	trustNamespace := trustManager.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = trustManagerOperandNamespace
	}
	updateNamespace(roleBinding, trustNamespace)
	updateResourceLabels(roleBinding, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.RoleBinding](roleBinding, serviceAccount, trustManagerOperandNamespace)
	return roleBinding
}

// createOrApplySecretTargetsRBAC creates additional RBAC rules when the SecretTargets policy
// is set to Custom. This grants trust-manager permissions to create and update the specific
// secrets listed in authorizedSecrets.
func (r *Reconciler) createOrApplySecretTargetsRBAC(trustManager *v1alpha1.TrustManager, serviceAccount string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	secretTargetsClusterRole := &rbacv1.ClusterRole{
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"secrets"},
				Verbs:         []string{"get", "list", "watch", "create", "update", "patch"},
				ResourceNames: trustManager.Spec.TrustManagerConfig.SecretTargets.AuthorizedSecrets,
			},
		},
	}
	secretTargetsClusterRole.SetName("trust-manager-secret-targets")
	secretTargetsClusterRole.SetLabels(resourceLabels)

	roleName := secretTargetsClusterRole.GetName()
	r.log.V(4).Info("reconciling secret targets clusterrole resource", "name", roleName)
	fetched := &rbacv1.ClusterRole{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(secretTargetsClusterRole), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s secret targets clusterrole resource already exists", roleName)
	}

	if exist && hasObjectChanged(secretTargetsClusterRole, fetched) {
		r.log.V(1).Info("secret targets clusterrole has been modified, updating to desired state", "name", roleName)
		if err := r.UpdateWithRetry(r.ctx, secretTargetsClusterRole); err != nil {
			return FromClientError(err, "failed to update %s secret targets clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "secret targets clusterrole resource %s reconciled back to desired state", roleName)
	}
	if !exist {
		if err := r.Create(r.ctx, secretTargetsClusterRole); err != nil {
			return FromClientError(err, "failed to create %s secret targets clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "secret targets clusterrole resource %s created", roleName)
	}

	// Create corresponding ClusterRoleBinding
	secretTargetsBinding := &rbacv1.ClusterRoleBinding{
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     roleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount,
				Namespace: trustManagerOperandNamespace,
			},
		},
	}
	secretTargetsBinding.SetName("trust-manager-secret-targets")
	secretTargetsBinding.SetLabels(resourceLabels)

	bindingName := secretTargetsBinding.GetName()
	r.log.V(4).Info("reconciling secret targets clusterrolebinding resource", "name", bindingName)
	fetchedBinding := &rbacv1.ClusterRoleBinding{}
	exist, err = r.Exists(r.ctx, client.ObjectKeyFromObject(secretTargetsBinding), fetchedBinding)
	if err != nil {
		return FromClientError(err, "failed to check %s secret targets clusterrolebinding resource already exists", bindingName)
	}

	if exist && hasObjectChanged(secretTargetsBinding, fetchedBinding) {
		r.log.V(1).Info("secret targets clusterrolebinding has been modified, updating to desired state", "name", bindingName)
		if err := r.UpdateWithRetry(r.ctx, secretTargetsBinding); err != nil {
			return FromClientError(err, "failed to update %s secret targets clusterrolebinding resource", bindingName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "secret targets clusterrolebinding resource %s reconciled back to desired state", bindingName)
	}
	if !exist {
		if err := r.Create(r.ctx, secretTargetsBinding); err != nil {
			return FromClientError(err, "failed to create %s secret targets clusterrolebinding resource", bindingName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "secret targets clusterrolebinding resource %s created", bindingName)
	}

	return nil
}
