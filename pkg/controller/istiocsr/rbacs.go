package istiocsr

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

const (
	roleBindingSubjectKind = "ServiceAccount"
)

func (r *Reconciler) createOrApplyRBACResource(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	serviceAccount := decodeServiceAccountObjBytes(assets.MustAsset(serviceAccountAssetName)).GetName()

	if err := r.createOrApplyClusterRoles(istiocsr, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile clusterrole resource")
		return err
	}

	if err := r.createOrApplyClusterRoleBindings(istiocsr, serviceAccount, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile clusterrolebinding resource")
		return err
	}

	if err := r.createOrApplyRoles(istiocsr, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile role resource")
		return err
	}

	if err := r.createOrApplyRoleBindings(istiocsr, serviceAccount, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile rolebinding resource")
		return err
	}

	if err := r.createOrApplyRoleForLeases(istiocsr, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile role for leases resource")
		return err
	}

	if err := r.createOrApplyRoleBindingForLeases(istiocsr, serviceAccount, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile rolebinding for leases resource")
		return err
	}

	return nil
}

func (r *Reconciler) createOrApplyClusterRoles(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getClusterRoleObject(istiocsr.GetNamespace(), resourceLabels)

	roleName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(1).Info("reconciling clusterrole resource", "name", roleName)
	fetched := &rbacv1.ClusterRole{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s clusterrole resource already exists", roleName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrole resource already exists, maybe from previous installation", roleName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("clusterrole has been modified, updating to desired state", "name", roleName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s reconciled back to desired state", roleName)
	} else {
		r.log.V(1).Info("clusterrole resource already exists and is in expected state", "name", roleName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s clusterrole resource", roleName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s created", roleName)
	}

	return nil
}

func (r *Reconciler) getClusterRoleObject(istioCSRNamespace string, resourceLabels map[string]string) *rbacv1.ClusterRole {
	clusterRole := decodeClusterRoleObjBytes(assets.MustAsset(clusterRoleAssetName))
	updateResourceLabelsWithIstioMapperLabels(clusterRole, istioCSRNamespace, resourceLabels)
	return clusterRole
}

func (r *Reconciler) createOrApplyClusterRoleBindings(istiocsr *v1alpha1.IstioCSR, serviceAccount string, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getClusterRoleBindingObject(serviceAccount, istiocsr.GetNamespace(), resourceLabels)

	var (
		exist           bool
		err             error
		roleBindingName string
		key             types.NamespacedName
		fetched         = &rbacv1.ClusterRoleBinding{}
	)
	r.log.V(1).Info("reconciling clusterrolebinding resource created for istiocsr", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())
	if istiocsr.Status.ClusterRoleBinding != "" {
		roleBindingName = fmt.Sprintf("%s/%s", desired.GetNamespace(), istiocsr.Status.ClusterRoleBinding)
		fetched = &rbacv1.ClusterRoleBinding{}
		key = types.NamespacedName{
			Name:      istiocsr.Status.ClusterRoleBinding,
			Namespace: desired.GetNamespace(),
		}
		exist, err = r.Exists(r.ctx, key, fetched)
		if err != nil {
			return FromClientError(err, "failed to check %s clusterrolebinding resource already exists", roleBindingName)
		}
	}
	if istiocsr.Status.ClusterRoleBinding == "" {
		// its possible updating the status might have failed, so will
		// resort to listing the resources and use the label selector to
		// make sure required resource does not exist already.
		clusterRoleBindingsList := &rbacv1.ClusterRoleBindingList{}
		if err := r.List(r.ctx, clusterRoleBindingsList, client.MatchingLabels(desired.GetLabels())); err != nil {
			return FromClientError(err, "failed to list clusterrolebinding resources, impacted namespace %s", istiocsr.GetNamespace())
		}
		if len(clusterRoleBindingsList.Items) > 0 {
			if len(clusterRoleBindingsList.Items) != 1 {
				r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "DuplicateResources", "more than 1 clusterrolebinding resources exist with matching labels")
				return NewIrrecoverableError(fmt.Errorf("more than 1 clusterrolebinding resources exist with matching labels"), "matched clusterrolebinding resources: %+v", clusterRoleBindingsList.Items)
			}
			clusterRoleBindingsList.Items[0].DeepCopyInto(fetched)

			roleBindingName = fmt.Sprintf("%s/%s", fetched.GetNamespace(), fetched.GetName())
			exist = true
		}
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrolebinding resource already exists, maybe from previous installation", roleBindingName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("clusterrolebinding has been modified, updating to desired state", "name", roleBindingName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s clusterrolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s reconciled back to desired state", roleBindingName)
	} else {
		r.log.V(1).Info("clusterrolebinding resource already exists and is in expected state", "name", roleBindingName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s clusterrolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s created", roleBindingName)
	}
	if err := r.updateClusterRoleBindingNameInStatus(istiocsr, desired, fetched); err != nil {
		return FromClientError(err, "failed to update %s/%s istiocsr status with %s clusterrolebinding resource name", istiocsr.GetNamespace(), istiocsr.GetName(), roleBindingName)
	}

	return nil
}

func (r *Reconciler) getClusterRoleBindingObject(serviceAccount, istiocsrNamespace string, resourceLabels map[string]string) *rbacv1.ClusterRoleBinding {
	clusterRoleBinding := decodeClusterRoleBindingObjBytes(assets.MustAsset(clusterRoleBindingAssetName))
	updateResourceLabelsWithIstioMapperLabels(clusterRoleBinding, istiocsrNamespace, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.ClusterRoleBinding](clusterRoleBinding, serviceAccount, istiocsrNamespace)
	return clusterRoleBinding
}

func (r *Reconciler) updateClusterRoleBindingNameInStatus(istiocsr *v1alpha1.IstioCSR, new, existing *rbacv1.ClusterRoleBinding) error {
	name := new.GetName()
	if name == "" {
		if existing != nil && existing.GetName() != "" {
			name = existing.GetName()
		} else {
			r.log.Error(fmt.Errorf("error updating clusterrolebinding name in status"), "istiocsr", istiocsr.GetNamespace())
		}
	}
	istiocsr.Status.ClusterRoleBinding = name
	return r.updateStatus(r.ctx, istiocsr)
}

func (r *Reconciler) createOrApplyRoles(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleObject(istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)

	roleName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(1).Info("reconciling role resource", "name", roleName)
	fetched := &rbacv1.Role{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s role resource already exists", roleName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s role resource already exists, maybe from previous installation", roleName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("role has been modified, updating to desired state", "name", roleName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s role resource", roleName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "role resource %s reconciled back to desired state", roleName)
	} else {
		r.log.V(1).Info("role resource already exists and is in expected state", "name", roleName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s role resource", roleName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "role resource %s created", roleName)
	}

	return nil
}

func (r *Reconciler) getRoleObject(istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleAssetName))
	updateNamespace(role, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(role, istiocsrNamespace, resourceLabels)
	return role
}

func (r *Reconciler) createOrApplyRoleBindings(istiocsr *v1alpha1.IstioCSR, serviceAccount string, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleBindingObject(serviceAccount, istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)

	roleBindingName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(1).Info("reconciling rolebinding resource", "name", roleBindingName)
	fetched := &rbacv1.RoleBinding{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s rolebinding resource already exists", roleBindingName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s rolebinding resource already exists, maybe from previous installation", roleBindingName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("rolebinding has been modified, updating to desired state", "name", roleBindingName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s rolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s reconciled back to desired state", roleBindingName)
	} else {
		r.log.V(1).Info("rolebinding resource already exists and is in expected state", "name", roleBindingName)

	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s rolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s created", roleBindingName)
	}

	return nil
}

func (r *Reconciler) getRoleBindingObject(serviceAccount, istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.RoleBinding {
	roleBinding := decodeRoleBindingObjBytes(assets.MustAsset(roleBindingAssetName))
	updateNamespace(roleBinding, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(roleBinding, istiocsrNamespace, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.RoleBinding](roleBinding, serviceAccount, istiocsrNamespace)
	return roleBinding
}

func (r *Reconciler) createOrApplyRoleForLeases(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleForLeasesObject(istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)

	roleName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(1).Info("reconciling role resource", "name", roleName)
	fetched := &rbacv1.Role{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s role resource already exists", roleName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s role resource already exists, maybe from previous installation", roleName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("role has been modified, updating to desired state", "name", roleName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s role resource", roleName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "role resource %s reconciled back to desired state", roleName)
	} else {
		r.log.V(1).Info("role resource already exists and is in expected state", "name", roleName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s role resource", roleName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "role resource %s created", roleName)
	}

	return nil
}

func (r *Reconciler) getRoleForLeasesObject(istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleLeasesAssetName))
	updateNamespace(role, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(role, istiocsrNamespace, resourceLabels)
	return role
}

func (r *Reconciler) createOrApplyRoleBindingForLeases(istiocsr *v1alpha1.IstioCSR, serviceAccount string, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleBindingForLeasesObject(serviceAccount, istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)

	roleBindingName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(1).Info("reconciling rolebinding resource", "name", roleBindingName)
	fetched := &rbacv1.RoleBinding{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s rolebinding resource already exists", roleBindingName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s rolebinding resource already exists, maybe from previous installation", roleBindingName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("rolebinding has been modified, updating to desired state", "name", roleBindingName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s rolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s reconciled back to desired state", roleBindingName)
	} else {
		r.log.V(1).Info("rolebinding resource already exists and is in expected state", "name", roleBindingName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s rolebinding resource", roleBindingName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s created", roleBindingName)
	}

	return nil
}

func (r *Reconciler) getRoleBindingForLeasesObject(serviceAccount, istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.RoleBinding {
	roleBinding := decodeRoleBindingObjBytes(assets.MustAsset(roleBindingLeasesAssetName))
	updateNamespace(roleBinding, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(roleBinding, istiocsrNamespace, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.RoleBinding](roleBinding, serviceAccount, istiocsrNamespace)
	return roleBinding
}

func updateServiceAccountNamespaceInRBACBindingObject[Object *rbacv1.RoleBinding | *rbacv1.ClusterRoleBinding](obj Object, serviceAccount, newNamespace string) {
	var subjects *[]rbacv1.Subject
	switch o := any(obj).(type) {
	case *rbacv1.ClusterRoleBinding:
		subjects = &o.Subjects
	case *rbacv1.RoleBinding:
		subjects = &o.Subjects
	}
	for i := range *subjects {
		if (*subjects)[i].Kind == roleBindingSubjectKind && (*subjects)[i].Name == serviceAccount {
			(*subjects)[i].Namespace = newNamespace
		}
	}
}
