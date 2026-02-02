package istiocsr

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

var (
	errMultipleClusterRolesExist           = errors.New("more than 1 clusterrole resources exist with matching labels")
	errErrorUpdatingClusterRoleName        = errors.New("error updating clusterrole name in status")
	errMultipleClusterRoleBindingsExist    = errors.New("more than 1 clusterrolebinding resources exist with matching labels")
	errErrorUpdatingClusterRoleBindingName = errors.New("error updating clusterrolebinding name in status")
)

const (
	roleBindingSubjectKind = "ServiceAccount"
)

func (r *Reconciler) createOrApplyRBACResource(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	serviceAccount := decodeServiceAccountObjBytes(assets.MustAsset(serviceAccountAssetName)).GetName()

	clusterRoleName, err := r.createOrApplyClusterRoles(istiocsr, resourceLabels, istioCSRCreateRecon)
	if err != nil {
		r.log.Error(err, "failed to reconcile clusterrole resource")
		return err
	}

	if err := r.createOrApplyClusterRoleBindings(istiocsr, clusterRoleName, serviceAccount, resourceLabels, istioCSRCreateRecon); err != nil {
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

func (r *Reconciler) createOrApplyClusterRoles(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) (string, error) {
	desired := r.getClusterRoleObject(istiocsr.GetNamespace(), resourceLabels)
	r.log.V(logVerbosityLevelDebug).Info("reconciling clusterrole resource created for istiocsr", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())

	exist, fetched, roleName, err := r.findExistingClusterRole(istiocsr, desired)
	if err != nil {
		return "", err
	}

	if err := r.reconcileClusterRoleResource(istiocsr, desired, fetched, roleName, exist, istioCSRCreateRecon); err != nil {
		return "", err
	}

	roleName, err = r.updateClusterRoleNameInStatus(istiocsr, desired, fetched)
	if err != nil {
		return "", FromClientError(err, "failed to update %s/%s istiocsr status with %s clusterrole resource name", istiocsr.GetNamespace(), istiocsr.GetName(), roleName)
	}

	return roleName, nil
}

func (r *Reconciler) findExistingClusterRole(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRole) (bool, *rbacv1.ClusterRole, string, error) {
	if istiocsr.Status.ClusterRole != "" {
		return r.findClusterRoleByStatus(istiocsr, desired)
	}
	return r.findClusterRoleByLabels(istiocsr, desired)
}

func (r *Reconciler) findClusterRoleByStatus(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRole) (bool, *rbacv1.ClusterRole, string, error) {
	roleName := fmt.Sprintf("%s/%s", desired.GetNamespace(), istiocsr.Status.ClusterRole)
	fetched := &rbacv1.ClusterRole{}
	key := client.ObjectKey{
		Name:      istiocsr.Status.ClusterRole,
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return false, nil, "", FromClientError(err, "failed to check %s clusterrole resource already exists", roleName)
	}
	return exist, fetched, roleName, nil
}

// findRBACResourceByLabels is a generic helper to find RBAC resources by labels.
// It handles the common logic of listing, checking for duplicates, and extracting the resource.
// The extractItems function should return the Items slice from the list (as pointers).
// The extractItemsForError function should return the Items slice from the list (as values) for error formatting.
func findRBACResourceByLabels[T client.Object, L client.ObjectList, V any](
	r *Reconciler,
	istiocsr *v1alpha1.IstioCSR,
	desired client.Object,
	list L,
	extractItems func(L) []T,
	extractItemsForError func(L) []V,
	resourceType string,
	duplicateErr error,
) (bool, T, string, error) {
	var zero T
	if err := r.List(r.ctx, list, client.MatchingLabels(desired.GetLabels())); err != nil {
		return false, zero, "", FromClientError(err, "failed to list %s resources, impacted namespace %s", resourceType, istiocsr.GetNamespace())
	}
	items := extractItems(list)
	if len(items) == 0 {
		return false, zero, "", nil
	}
	if len(items) != 1 {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "DuplicateResources", "more than 1 %s resources exist with matching labels", resourceType)
		// Use the original list items (values) for error formatting to match expected format
		itemsForError := extractItemsForError(list)
		return false, zero, "", NewIrrecoverableError(duplicateErr, "matched %s resources: %+v", resourceType, itemsForError)
	}
	fetched := items[0]
	resourceName := fmt.Sprintf("%s/%s", fetched.GetNamespace(), fetched.GetName())
	return true, fetched, resourceName, nil
}

func (r *Reconciler) findClusterRoleByLabels(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRole) (bool, *rbacv1.ClusterRole, string, error) {
	clusterRoleList := &rbacv1.ClusterRoleList{}
	exist, fetched, roleName, err := findRBACResourceByLabels(r, istiocsr, desired, clusterRoleList,
		func(l *rbacv1.ClusterRoleList) []*rbacv1.ClusterRole {
			items := make([]*rbacv1.ClusterRole, len(l.Items))
			for i := range l.Items {
				items[i] = &l.Items[i]
			}
			return items
		},
		func(l *rbacv1.ClusterRoleList) []rbacv1.ClusterRole {
			return l.Items
		},
		"clusterrole", errMultipleClusterRolesExist)
	if err != nil {
		return false, nil, "", err
	}
	if !exist {
		return false, nil, "", nil
	}
	// Deep copy to avoid returning a reference to the list item
	result := &rbacv1.ClusterRole{}
	fetched.DeepCopyInto(result)
	return true, result, roleName, nil
}

func (r *Reconciler) reconcileClusterRoleResource(istiocsr *v1alpha1.IstioCSR, desired, fetched *rbacv1.ClusterRole, roleName string, exist, istioCSRCreateRecon bool) error {
	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrole resource already exists, maybe from previous installation", roleName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		// Copy Name and ResourceVersion from fetched to desired for generateName resources.
		// When resources are created with generateName, desired.Name is empty (cleared by updateToUseGenerateName).
		// UpdateWithRetry requires Name and ResourceVersion to construct a valid ObjectKey and perform the update.
		desired.SetName(fetched.GetName())
		desired.SetResourceVersion(fetched.GetResourceVersion())
		return r.updateClusterRole(istiocsr, desired, roleName)
	}
	if exist {
		r.log.V(logVerbosityLevelDebug).Info("clusterrole resource already exists and is in expected state", "name", roleName)
		return nil
	}
	return r.createClusterRole(istiocsr, desired, roleName)
}

func (r *Reconciler) updateClusterRole(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRole, roleName string) error {
	r.log.V(1).Info("clusterrole has been modified, updating to desired state", "name", roleName)
	if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
		return FromClientError(err, "failed to update %s clusterrole resource", roleName)
	}
	r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s reconciled back to desired state", roleName)
	return nil
}

func (r *Reconciler) createClusterRole(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRole, roleName string) error {
	if err := r.Create(r.ctx, desired); err != nil {
		return FromClientError(err, "failed to create %s clusterrole resource", roleName)
	}
	r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s created", roleName)
	return nil
}

func (r *Reconciler) getClusterRoleObject(istioCSRNamespace string, resourceLabels map[string]string) *rbacv1.ClusterRole {
	clusterRole := decodeClusterRoleObjBytes(assets.MustAsset(clusterRoleAssetName))
	updateToUseGenerateName(clusterRole)
	updateResourceLabelsWithIstioMapperLabels(clusterRole, istioCSRNamespace, resourceLabels)
	return clusterRole
}

func updateToUseGenerateName(obj client.Object) {
	obj.SetName("")
	obj.SetGenerateName("cert-manager-istio-csr-")
}

func (r *Reconciler) updateClusterRoleNameInStatus(istiocsr *v1alpha1.IstioCSR, desired, existing *rbacv1.ClusterRole) (string, error) {
	name := desired.GetName()
	if name == "" {
		if existing != nil && existing.GetName() != "" {
			name = existing.GetName()
		} else {
			r.log.Error(errErrorUpdatingClusterRoleName, "istiocsr", istiocsr.GetNamespace())
		}
	}
	istiocsr.Status.ClusterRole = name
	return name, r.updateStatus(r.ctx, istiocsr)
}

func (r *Reconciler) createOrApplyClusterRoleBindings(istiocsr *v1alpha1.IstioCSR, clusterRoleName, serviceAccount string, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getClusterRoleBindingObject(clusterRoleName, serviceAccount, istiocsr.GetNamespace(), resourceLabels)
	r.log.V(logVerbosityLevelDebug).Info("reconciling clusterrolebinding resource created for istiocsr", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())

	exist, fetched, roleBindingName, err := r.findExistingClusterRoleBinding(istiocsr, desired)
	if err != nil {
		return err
	}

	if err := r.reconcileClusterRoleBindingResource(istiocsr, desired, fetched, roleBindingName, exist, istioCSRCreateRecon); err != nil {
		return err
	}

	if err := r.updateClusterRoleBindingNameInStatus(istiocsr, desired, fetched); err != nil {
		return FromClientError(err, "failed to update %s/%s istiocsr status with %s clusterrolebinding resource name", istiocsr.GetNamespace(), istiocsr.GetName(), roleBindingName)
	}

	return nil
}

func (r *Reconciler) findExistingClusterRoleBinding(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRoleBinding) (bool, *rbacv1.ClusterRoleBinding, string, error) {
	if istiocsr.Status.ClusterRoleBinding != "" {
		return r.findClusterRoleBindingByStatus(istiocsr, desired)
	}
	return r.findClusterRoleBindingByLabels(istiocsr, desired)
}

func (r *Reconciler) findClusterRoleBindingByStatus(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRoleBinding) (bool, *rbacv1.ClusterRoleBinding, string, error) {
	roleBindingName := fmt.Sprintf("%s/%s", desired.GetNamespace(), istiocsr.Status.ClusterRoleBinding)
	fetched := &rbacv1.ClusterRoleBinding{}
	key := client.ObjectKey{
		Name:      istiocsr.Status.ClusterRoleBinding,
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return false, nil, "", FromClientError(err, "failed to check %s clusterrolebinding resource already exists", roleBindingName)
	}
	return exist, fetched, roleBindingName, nil
}

func (r *Reconciler) findClusterRoleBindingByLabels(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRoleBinding) (bool, *rbacv1.ClusterRoleBinding, string, error) {
	clusterRoleBindingsList := &rbacv1.ClusterRoleBindingList{}
	exist, fetched, roleBindingName, err := findRBACResourceByLabels(r, istiocsr, desired, clusterRoleBindingsList,
		func(l *rbacv1.ClusterRoleBindingList) []*rbacv1.ClusterRoleBinding {
			items := make([]*rbacv1.ClusterRoleBinding, len(l.Items))
			for i := range l.Items {
				items[i] = &l.Items[i]
			}
			return items
		},
		func(l *rbacv1.ClusterRoleBindingList) []rbacv1.ClusterRoleBinding {
			return l.Items
		},
		"clusterrolebinding", errMultipleClusterRoleBindingsExist)
	if err != nil {
		return false, nil, "", err
	}
	if !exist {
		return false, nil, "", nil
	}
	// Deep copy to avoid returning a reference to the list item
	result := &rbacv1.ClusterRoleBinding{}
	fetched.DeepCopyInto(result)
	return true, result, roleBindingName, nil
}

func (r *Reconciler) reconcileClusterRoleBindingResource(istiocsr *v1alpha1.IstioCSR, desired, fetched *rbacv1.ClusterRoleBinding, roleBindingName string, exist, istioCSRCreateRecon bool) error {
	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s clusterrolebinding resource already exists, maybe from previous installation", roleBindingName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		// Copy Name and ResourceVersion from fetched to desired for generateName resources.
		// When resources are created with generateName, desired.Name is empty (cleared by updateToUseGenerateName).
		// UpdateWithRetry requires Name and ResourceVersion to construct a valid ObjectKey and perform the update.
		desired.SetName(fetched.GetName())
		desired.SetResourceVersion(fetched.GetResourceVersion())
		return r.updateClusterRoleBinding(istiocsr, desired, roleBindingName)
	}
	if exist {
		r.log.V(logVerbosityLevelDebug).Info("clusterrolebinding resource already exists and is in expected state", "name", roleBindingName)
		return nil
	}
	return r.createClusterRoleBinding(istiocsr, desired, roleBindingName)
}

func (r *Reconciler) updateClusterRoleBinding(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRoleBinding, roleBindingName string) error {
	r.log.V(1).Info("clusterrolebinding has been modified, updating to desired state", "name", roleBindingName)
	if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
		return FromClientError(err, "failed to update %s clusterrolebinding resource", roleBindingName)
	}
	r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s reconciled back to desired state", roleBindingName)
	return nil
}

func (r *Reconciler) createClusterRoleBinding(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.ClusterRoleBinding, roleBindingName string) error {
	if err := r.Create(r.ctx, desired); err != nil {
		return FromClientError(err, "failed to create %s clusterrolebinding resource", roleBindingName)
	}
	r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s created", roleBindingName)
	return nil
}

func (r *Reconciler) getClusterRoleBindingObject(clusterRoleName, serviceAccount, istiocsrNamespace string, resourceLabels map[string]string) *rbacv1.ClusterRoleBinding {
	clusterRoleBinding := decodeClusterRoleBindingObjBytes(assets.MustAsset(clusterRoleBindingAssetName))
	clusterRoleBinding.RoleRef.Name = clusterRoleName
	updateToUseGenerateName(clusterRoleBinding)
	updateResourceLabelsWithIstioMapperLabels(clusterRoleBinding, istiocsrNamespace, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.ClusterRoleBinding](clusterRoleBinding, serviceAccount, istiocsrNamespace)
	return clusterRoleBinding
}

func (r *Reconciler) updateClusterRoleBindingNameInStatus(istiocsr *v1alpha1.IstioCSR, desired, existing *rbacv1.ClusterRoleBinding) error {
	name := desired.GetName()
	if name == "" {
		if existing != nil && existing.GetName() != "" {
			name = existing.GetName()
		} else {
			r.log.Error(errErrorUpdatingClusterRoleBindingName, "istiocsr", istiocsr.GetNamespace())
		}
	}
	istiocsr.Status.ClusterRoleBinding = name
	return r.updateStatus(r.ctx, istiocsr)
}

func (r *Reconciler) createOrApplyRoles(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleObject(istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)
	return r.reconcileRole(istiocsr, desired, "role resource", istioCSRCreateRecon)
}

func (r *Reconciler) getRoleObject(istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleAssetName))
	updateNamespace(role, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(role, istiocsrNamespace, resourceLabels)
	return role
}

func (r *Reconciler) reconcileRole(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.Role, resourceDescription string, istioCSRCreateRecon bool) error {
	return r.reconcileRBACResource(istiocsr, desired, &rbacv1.Role{}, resourceDescription, "role", istioCSRCreateRecon)
}

func (r *Reconciler) createOrApplyRoleBindings(istiocsr *v1alpha1.IstioCSR, serviceAccount string, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleBindingObject(serviceAccount, istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)
	return r.reconcileRoleBinding(istiocsr, desired, "rolebinding resource", istioCSRCreateRecon)
}

func (r *Reconciler) getRoleBindingObject(serviceAccount, istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.RoleBinding {
	roleBinding := decodeRoleBindingObjBytes(assets.MustAsset(roleBindingAssetName))
	updateNamespace(roleBinding, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(roleBinding, istiocsrNamespace, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.RoleBinding](roleBinding, serviceAccount, istiocsrNamespace)
	return roleBinding
}

func (r *Reconciler) reconcileRoleBinding(istiocsr *v1alpha1.IstioCSR, desired *rbacv1.RoleBinding, resourceDescription string, istioCSRCreateRecon bool) error {
	return r.reconcileRBACResource(istiocsr, desired, &rbacv1.RoleBinding{}, resourceDescription, "rolebinding", istioCSRCreateRecon)
}

func (r *Reconciler) reconcileRBACResource(istiocsr *v1alpha1.IstioCSR, desired client.Object, fetched client.Object, resourceDescription, resourceType string, istioCSRCreateRecon bool) error {
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(logVerbosityLevelDebug).Info("reconciling "+resourceDescription, "name", resourceName)
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s %s resource already exists", resourceName, resourceType)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s %s resource already exists, maybe from previous installation", resourceName, resourceType)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info(resourceType+" has been modified, updating to desired state", "name", resourceName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s %s resource", resourceName, resourceType)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "%s resource %s reconciled back to desired state", resourceType, resourceName)
	} else if exist {
		r.log.V(logVerbosityLevelDebug).Info(resourceType+" resource already exists and is in expected state", "name", resourceName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s %s resource", resourceName, resourceType)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "%s resource %s created", resourceType, resourceName)
	}

	return nil
}

func (r *Reconciler) createOrApplyRoleForLeases(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleForLeasesObject(istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)
	return r.reconcileRole(istiocsr, desired, "role for lease resource", istioCSRCreateRecon)
}

func (r *Reconciler) getRoleForLeasesObject(istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleLeasesAssetName))
	updateNamespace(role, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(role, istiocsrNamespace, resourceLabels)
	return role
}

func (r *Reconciler) createOrApplyRoleBindingForLeases(istiocsr *v1alpha1.IstioCSR, serviceAccount string, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleBindingForLeasesObject(serviceAccount, istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)
	return r.reconcileRoleBinding(istiocsr, desired, "rolebinding for lease resource", istioCSRCreateRecon)
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
