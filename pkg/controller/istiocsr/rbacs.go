package istiocsr

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

var (
	errMoreThanOneClusterRole         = errors.New("more than 1 clusterrole resources exist with matching labels")
	errUpdateClusterRoleStatus        = errors.New("error updating clusterrole name in status")
	errMoreThanOneClusterRoleBinding  = errors.New("more than 1 clusterrolebinding resources exist with matching labels")
	errUpdateClusterRoleBindingStatus = errors.New("error updating clusterrolebinding name in status")
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

// resolveExistingClusterScopedResource finds an existing cluster-scoped resource by status name
// or by label selector. statusName is the name stored in the IstioCSR status (e.g.,
// istiocsr.Status.ClusterRole). listAndResolve is called when statusName is empty; it should list
// matching resources by label, copy the single matched item into fetched, and return (true, nil).
// If no items match, return (false, nil).
func (r *Reconciler) resolveExistingClusterScopedResource(
	_ *v1alpha1.IstioCSR,
	desired, fetched client.Object,
	statusName, resourceKind string,
	listAndResolve func() (bool, error),
) (bool, error) {
	if statusName != "" {
		key := client.ObjectKey{
			Name:      statusName,
			Namespace: desired.GetNamespace(),
		}
		exist, err := r.Exists(r.ctx, key, fetched)
		if err != nil {
			name := fmt.Sprintf("%s/%s", desired.GetNamespace(), statusName)
			return false, common.FromClientError(err, "failed to check %s %s resource already exists", name, resourceKind)
		}
		return exist, nil
	}

	// its possible updating the status might have failed, so will
	// resort to listing the resources and use the label selector to
	// make sure required resource does not exist already.
	return listAndResolve()
}

func (r *Reconciler) createOrApplyClusterRoles(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) (string, error) {
	desired := r.getClusterRoleObject(istiocsr.GetNamespace(), resourceLabels)

	r.log.V(4).Info("reconciling clusterrole resource created for istiocsr", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())
	fetched := &rbacv1.ClusterRole{}
	exist, err := r.resolveExistingClusterScopedResource(
		istiocsr, desired, fetched,
		istiocsr.Status.ClusterRole, "clusterrole",
		func() (bool, error) {
			list := &rbacv1.ClusterRoleList{}
			if err := r.List(r.ctx, list, client.MatchingLabels(desired.GetLabels())); err != nil {
				return false, common.FromClientError(err, "failed to list clusterrole resources, impacted namespace %s", istiocsr.GetNamespace())
			}
			if len(list.Items) == 0 {
				return false, nil
			}
			if len(list.Items) != 1 {
				r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "DuplicateResources", "more than 1 clusterrole resources exist with matching labels")
				return false, common.NewIrrecoverableError(errMoreThanOneClusterRole, "matched clusterrole resources: %+v", list.Items)
			}
			list.Items[0].DeepCopyInto(fetched)
			return true, nil
		},
	)
	if err != nil {
		return "", err
	}

	var roleName string
	switch {
	case istiocsr.Status.ClusterRole != "":
		roleName = fmt.Sprintf("%s/%s", desired.GetNamespace(), istiocsr.Status.ClusterRole)
	case exist:
		roleName = fmt.Sprintf("%s/%s", fetched.GetNamespace(), fetched.GetName())
	default:
		roleName = desired.GetGenerateName()
	}

	if err := r.applyResource(istiocsr, desired, fetched, roleName, "clusterrole", exist, istioCSRCreateRecon); err != nil {
		return "", err
	}
	if roleName, err = r.updateClusterRoleNameInStatus(istiocsr, desired, fetched); err != nil {
		return "", common.FromClientError(err, "failed to update %s/%s istiocsr status with %s clusterrole resource name", istiocsr.GetNamespace(), istiocsr.GetName(), roleName)
	}

	return roleName, nil
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
				r.log.Error(errUpdateClusterRoleStatus, "istiocsr", istiocsr.GetNamespace())
			}
		}
		istiocsr.Status.ClusterRole = name
	return name, r.updateStatus(r.ctx, istiocsr)
}

func (r *Reconciler) createOrApplyClusterRoleBindings(istiocsr *v1alpha1.IstioCSR, clusterRoleName, serviceAccount string, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getClusterRoleBindingObject(clusterRoleName, serviceAccount, istiocsr.GetNamespace(), resourceLabels)

	r.log.V(4).Info("reconciling clusterrolebinding resource created for istiocsr", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())
	fetched := &rbacv1.ClusterRoleBinding{}
	exist, err := r.resolveExistingClusterScopedResource(
		istiocsr, desired, fetched,
		istiocsr.Status.ClusterRoleBinding, "clusterrolebinding",
		func() (bool, error) {
			list := &rbacv1.ClusterRoleBindingList{}
			if err := r.List(r.ctx, list, client.MatchingLabels(desired.GetLabels())); err != nil {
				return false, common.FromClientError(err, "failed to list clusterrolebinding resources, impacted namespace %s", istiocsr.GetNamespace())
			}
			if len(list.Items) == 0 {
				return false, nil
			}
			if len(list.Items) != 1 {
				r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "DuplicateResources", "more than 1 clusterrolebinding resources exist with matching labels")
				return false, common.NewIrrecoverableError(errMoreThanOneClusterRoleBinding, "matched clusterrolebinding resources: %+v", list.Items)
			}
			list.Items[0].DeepCopyInto(fetched)
			return true, nil
		},
	)
	if err != nil {
		return err
	}

	var roleBindingName string
	switch {
	case istiocsr.Status.ClusterRoleBinding != "":
		roleBindingName = fmt.Sprintf("%s/%s", desired.GetNamespace(), istiocsr.Status.ClusterRoleBinding)
	case exist:
		roleBindingName = fmt.Sprintf("%s/%s", fetched.GetNamespace(), fetched.GetName())
	default:
		roleBindingName = desired.GetGenerateName()
	}

	if err := r.applyResource(istiocsr, desired, fetched, roleBindingName, "clusterrolebinding", exist, istioCSRCreateRecon); err != nil {
		return err
	}
	if err := r.updateClusterRoleBindingNameInStatus(istiocsr, desired, fetched); err != nil {
		return common.FromClientError(err, "failed to update %s/%s istiocsr status with %s clusterrolebinding resource name", istiocsr.GetNamespace(), istiocsr.GetName(), roleBindingName)
	}

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
				r.log.Error(errUpdateClusterRoleBindingStatus, "istiocsr", istiocsr.GetNamespace())
			}
		}
		istiocsr.Status.ClusterRoleBinding = name
	return r.updateStatus(r.ctx, istiocsr)
}

func (r *Reconciler) createOrApplyRoles(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleObject(istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)
	return r.reconcileNamespacedRBACObject(istiocsr, desired, &rbacv1.Role{}, "reconciling role resource", "role resource", istioCSRCreateRecon)
}

func (r *Reconciler) getRoleObject(istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleAssetName))
	common.UpdateNamespace(role, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(role, istiocsrNamespace, resourceLabels)
	return role
}

func (r *Reconciler) createOrApplyRoleBindings(istiocsr *v1alpha1.IstioCSR, serviceAccount string, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleBindingObject(serviceAccount, istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)
	return r.reconcileNamespacedRBACObject(istiocsr, desired, &rbacv1.RoleBinding{}, "reconciling rolebinding resource", "rolebinding resource", istioCSRCreateRecon)
}

func (r *Reconciler) getRoleBindingObject(serviceAccount, istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.RoleBinding {
	roleBinding := decodeRoleBindingObjBytes(assets.MustAsset(roleBindingAssetName))
	common.UpdateNamespace(roleBinding, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(roleBinding, istiocsrNamespace, resourceLabels)
	updateServiceAccountNamespaceInRBACBindingObject[*rbacv1.RoleBinding](roleBinding, serviceAccount, istiocsrNamespace)
	return roleBinding
}

func (r *Reconciler) createOrApplyRoleForLeases(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleForLeasesObject(istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)
	return r.reconcileNamespacedRBACObject(istiocsr, desired, &rbacv1.Role{}, "reconciling role for lease resource", "role for lease resource", istioCSRCreateRecon)
}

func (r *Reconciler) getRoleForLeasesObject(istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleLeasesAssetName))
	common.UpdateNamespace(role, roleNamespace)
	updateResourceLabelsWithIstioMapperLabels(role, istiocsrNamespace, resourceLabels)
	return role
}

func (r *Reconciler) createOrApplyRoleBindingForLeases(istiocsr *v1alpha1.IstioCSR, serviceAccount string, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getRoleBindingForLeasesObject(serviceAccount, istiocsr.GetNamespace(), istiocsr.Spec.IstioCSRConfig.Istio.Namespace, resourceLabels)
	return r.reconcileNamespacedRBACObject(istiocsr, desired, &rbacv1.RoleBinding{}, "reconciling rolebinding for lease resource", "rolebinding for lease resource", istioCSRCreateRecon)
}

// reconcileNamespacedRBACObject handles the common create-or-update logic for namespaced RBAC
// resources (Role and RoleBinding). logMsg is used for the initial reconciliation log; resourceKind
// is used in error and event messages. fetched must be an empty instance of the same type as desired.
func (r *Reconciler) reconcileNamespacedRBACObject(istiocsr *v1alpha1.IstioCSR, desired, fetched client.Object, logMsg, resourceKind string, istioCSRCreateRecon bool) error {
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info(logMsg, "name", resourceName)
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return common.FromClientError(err, "failed to check %s %s already exists", resourceName, resourceKind)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s %s already exists, maybe from previous installation", resourceName, resourceKind)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info(resourceKind+" has been modified, updating to desired state", "name", resourceName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return common.FromClientError(err, "failed to update %s %s", resourceName, resourceKind)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "%s %s reconciled back to desired state", resourceKind, resourceName)
	} else {
		r.log.V(4).Info(resourceKind+" already exists and is in expected state", "name", resourceName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return common.FromClientError(err, "failed to create %s %s", resourceName, resourceKind)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "%s %s created", resourceKind, resourceName)
	}

	return nil
}

func (r *Reconciler) getRoleBindingForLeasesObject(serviceAccount, istiocsrNamespace, roleNamespace string, resourceLabels map[string]string) *rbacv1.RoleBinding {
	roleBinding := decodeRoleBindingObjBytes(assets.MustAsset(roleBindingLeasesAssetName))
	common.UpdateNamespace(roleBinding, roleNamespace)
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
