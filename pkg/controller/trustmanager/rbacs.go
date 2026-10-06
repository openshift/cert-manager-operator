package trustmanager

import (
	"fmt"
	"reflect"
	"slices"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyRBACResources(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string, trustNamespace string) error {
	if err := r.createOrApplyClusterRole(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile clusterrole resource")
		return err
	}

	if err := r.createOrApplyClusterRoleBinding(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile clusterrolebinding resource")
		return err
	}

	if err := r.createOrApplyTrustNamespaceRole(trustManager, resourceLabels, resourceAnnotations, trustNamespace); err != nil {
		r.log.Error(err, "failed to reconcile role resource for trust namespace")
		return err
	}

	if err := r.createOrApplyTrustNamespaceRoleBinding(trustManager, resourceLabels, resourceAnnotations, trustNamespace); err != nil {
		r.log.Error(err, "failed to reconcile rolebinding resource for trust namespace")
		return err
	}

	if err := r.createOrApplyLeaderElectionRole(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile leader election role resource")
		return err
	}

	if err := r.createOrApplyLeaderElectionRoleBinding(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile leader election rolebinding resource")
		return err
	}

	if err := r.createOrApplyTargetNamespaceRBAC(trustManager, resourceLabels, resourceAnnotations, trustNamespace); err != nil {
		r.log.Error(err, "failed to reconcile target namespace RBAC resources")
		return err
	}

	if err := r.cleanupStaleTargetNamespaceRBAC(trustManager, trustNamespace); err != nil {
		r.log.Error(err, "failed to clean up stale target namespace RBAC resources")
		return err
	}

	return nil
}

// ClusterRole

func (r *Reconciler) createOrApplyClusterRole(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getClusterRoleObject(trustManager.Spec.TrustManagerConfig, resourceLabels, resourceAnnotations)
	resourceName := desired.GetName()
	r.log.V(4).Info("reconciling clusterrole resource", "name", resourceName)

	existing := &rbacv1.ClusterRole{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if clusterrole %q exists", resourceName)
	}
	if exists && !clusterRoleModified(desired, existing) {
		r.log.V(4).Info("clusterrole resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("clusterrole resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply clusterrole %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s applied", resourceName)
	return nil
}

func getClusterRoleObject(config v1alpha1.TrustManagerConfig, resourceLabels, resourceAnnotations map[string]string) *rbacv1.ClusterRole {
	clusterRole := common.DecodeObjBytes[*rbacv1.ClusterRole](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(clusterRoleAssetName))
	common.UpdateName(clusterRole, trustManagerClusterRoleName)
	common.UpdateResourceLabels(clusterRole, resourceLabels)
	updateResourceAnnotations(clusterRole, resourceAnnotations)
	if len(config.TargetNamespaces) > 0 {
		// Match upstream Helm: drop cluster-wide ConfigMap/Event write and
		// emit those rules as per-namespace Roles instead.
		clusterRole.Rules = slices.DeleteFunc(clusterRole.Rules, isNamespacedTargetRule)
	} else {
		appendSecretTargetRules(&clusterRole.Rules, config.SecretTargets)
	}
	return clusterRole
}

// isNamespacedTargetRule reports whether the rule is the ConfigMap or Event
// write that Helm moves onto per-namespace Roles when targetNamespaces is set.
func isNamespacedTargetRule(rule rbacv1.PolicyRule) bool {
	return slices.Contains(rule.Resources, "configmaps") || slices.Contains(rule.Resources, "events")
}

// appendSecretTargetRules adds secret read and scoped write rules when the
// secretTargets policy is Custom. The authorizedSecrets list is sorted to
// ensure deterministic rule ordering for comparison.
func appendSecretTargetRules(rules *[]rbacv1.PolicyRule, secretTargets v1alpha1.SecretTargetsConfig) {
	if !secretTargetsEnabled(secretTargets) {
		return
	}

	*rules = append(*rules, rbacv1.PolicyRule{
		APIGroups: []string{""},
		Resources: []string{"secrets"},
		Verbs:     []string{"get", "list", "watch"},
	})

	sortedSecrets := slices.Clone(secretTargets.AuthorizedSecrets)
	slices.Sort(sortedSecrets)

	*rules = append(*rules, rbacv1.PolicyRule{
		APIGroups:     []string{""},
		Resources:     []string{"secrets"},
		ResourceNames: sortedSecrets,
		Verbs:         []string{"create", "update", "patch", "delete"},
	})
}

// namespacedTargetRules is the ConfigMap write (+ Events, optional Secrets)
// granted in each target namespace when targetNamespaces is set.
func namespacedTargetRules(secretTargets v1alpha1.SecretTargetsConfig) []rbacv1.PolicyRule {
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get", "list", "create", "patch", "watch", "delete"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"events"},
			Verbs:     []string{"create", "patch"},
		},
	}
	appendSecretTargetRules(&rules, secretTargets)
	return rules
}

// ClusterRoleBinding

func (r *Reconciler) createOrApplyClusterRoleBinding(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getClusterRoleBindingObject(resourceLabels, resourceAnnotations)
	resourceName := desired.GetName()
	r.log.V(4).Info("reconciling clusterrolebinding resource", "name", resourceName)

	existing := &rbacv1.ClusterRoleBinding{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if clusterrolebinding %q exists", resourceName)
	}
	if exists && !clusterRoleBindingModified(desired, existing) {
		r.log.V(4).Info("clusterrolebinding resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("clusterrolebinding resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply clusterrolebinding %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s applied", resourceName)
	return nil
}

func getClusterRoleBindingObject(resourceLabels, resourceAnnotations map[string]string) *rbacv1.ClusterRoleBinding {
	clusterRoleBinding := common.DecodeObjBytes[*rbacv1.ClusterRoleBinding](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(clusterRoleBindingAssetName))
	common.UpdateName(clusterRoleBinding, trustManagerClusterRoleBindingName)
	common.UpdateResourceLabels(clusterRoleBinding, resourceLabels)
	updateResourceAnnotations(clusterRoleBinding, resourceAnnotations)
	clusterRoleBinding.RoleRef.Name = trustManagerClusterRoleName
	updateBindingSubjects(clusterRoleBinding.Subjects, trustManagerServiceAccountName, operandNamespace)
	return clusterRoleBinding
}

// Role for trust namespace (secrets access)

func (r *Reconciler) createOrApplyTrustNamespaceRole(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string, trustNamespace string) error {
	desired := getTrustNamespaceRoleObject(resourceLabels, resourceAnnotations, trustNamespace)
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling role resource for trust namespace", "name", resourceName)

	existing := &rbacv1.Role{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if role %q exists", resourceName)
	}
	if exists && !roleModified(desired, existing) {
		r.log.V(4).Info("role resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("role resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply role %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "role resource %s applied", resourceName)
	return nil
}

func getTrustNamespaceRoleObject(resourceLabels, resourceAnnotations map[string]string, trustNamespace string) *rbacv1.Role {
	role := common.DecodeObjBytes[*rbacv1.Role](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(roleAssetName))
	common.UpdateName(role, trustManagerRoleName)
	common.UpdateNamespace(role, trustNamespace)
	common.UpdateResourceLabels(role, resourceLabels)
	updateResourceAnnotations(role, resourceAnnotations)
	return role
}

// RoleBinding for trust namespace (secrets access)

func (r *Reconciler) createOrApplyTrustNamespaceRoleBinding(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string, trustNamespace string) error {
	desired := getTrustNamespaceRoleBindingObject(resourceLabels, resourceAnnotations, trustNamespace)
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling rolebinding resource for trust namespace", "name", resourceName)

	existing := &rbacv1.RoleBinding{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if rolebinding %q exists", resourceName)
	}
	if exists && !roleBindingModified(desired, existing) {
		r.log.V(4).Info("rolebinding resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("rolebinding resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply rolebinding %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "rolebinding resource %s applied", resourceName)
	return nil
}

func getTrustNamespaceRoleBindingObject(resourceLabels, resourceAnnotations map[string]string, trustNamespace string) *rbacv1.RoleBinding {
	roleBinding := common.DecodeObjBytes[*rbacv1.RoleBinding](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(roleBindingAssetName))
	common.UpdateName(roleBinding, trustManagerRoleBindingName)
	common.UpdateNamespace(roleBinding, trustNamespace)
	common.UpdateResourceLabels(roleBinding, resourceLabels)
	updateResourceAnnotations(roleBinding, resourceAnnotations)
	roleBinding.RoleRef.Name = trustManagerRoleName
	updateBindingSubjects(roleBinding.Subjects, trustManagerServiceAccountName, operandNamespace)
	return roleBinding
}

// Leader election Role (in operand namespace)

func (r *Reconciler) createOrApplyLeaderElectionRole(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getLeaderElectionRoleObject(resourceLabels, resourceAnnotations)
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling leader election role resource", "name", resourceName)

	existing := &rbacv1.Role{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if leader election role %q exists", resourceName)
	}
	if exists && !roleModified(desired, existing) {
		r.log.V(4).Info("leader election role resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("leader election role resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply leader election role %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "leader election role resource %s applied", resourceName)
	return nil
}

func getLeaderElectionRoleObject(resourceLabels, resourceAnnotations map[string]string) *rbacv1.Role {
	role := common.DecodeObjBytes[*rbacv1.Role](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(roleLeaderElectionAssetName))
	common.UpdateName(role, trustManagerLeaderElectionRoleName)
	common.UpdateNamespace(role, operandNamespace)
	common.UpdateResourceLabels(role, resourceLabels)
	updateResourceAnnotations(role, resourceAnnotations)
	return role
}

// Leader election RoleBinding (in operand namespace)

func (r *Reconciler) createOrApplyLeaderElectionRoleBinding(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getLeaderElectionRoleBindingObject(resourceLabels, resourceAnnotations)
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling leader election rolebinding resource", "name", resourceName)

	existing := &rbacv1.RoleBinding{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if leader election rolebinding %q exists", resourceName)
	}
	if exists && !roleBindingModified(desired, existing) {
		r.log.V(4).Info("leader election rolebinding resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("leader election rolebinding resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply leader election rolebinding %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "leader election rolebinding resource %s applied", resourceName)
	return nil
}

func getLeaderElectionRoleBindingObject(resourceLabels, resourceAnnotations map[string]string) *rbacv1.RoleBinding {
	roleBinding := common.DecodeObjBytes[*rbacv1.RoleBinding](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(roleBindingLeaderElectionAssetName))
	common.UpdateName(roleBinding, trustManagerLeaderElectionRoleBindingName)
	common.UpdateNamespace(roleBinding, operandNamespace)
	common.UpdateResourceLabels(roleBinding, resourceLabels)
	updateResourceAnnotations(roleBinding, resourceAnnotations)
	roleBinding.RoleRef.Name = trustManagerLeaderElectionRoleName
	updateBindingSubjects(roleBinding.Subjects, trustManagerServiceAccountName, operandNamespace)
	return roleBinding
}

// Per-namespace Role/RoleBinding for Bundle target writes when targetNamespaces is set.

func (r *Reconciler) createOrApplyTargetNamespaceRBAC(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string, trustNamespace string) error {
	for _, ns := range targetRBACNamespaces(trustManager, trustNamespace) {
		if err := r.createOrApplyTargetNamespaceRole(trustManager, resourceLabels, resourceAnnotations, ns); err != nil {
			return err
		}
		if err := r.createOrApplyTargetNamespaceRoleBinding(trustManager, resourceLabels, resourceAnnotations, ns); err != nil {
			return err
		}
	}
	return nil
}

// targetRBACNamespaces is the unique, sorted set of namespaces that receive
// namespaced target write Roles: listed targetNamespaces plus trustNamespace
// (upstream cache always includes the trust namespace).
func targetRBACNamespaces(trustManager *v1alpha1.TrustManager, trustNamespace string) []string {
	if len(trustManager.Spec.TrustManagerConfig.TargetNamespaces) == 0 {
		return nil
	}
	ns := slices.Clone(trustManager.Spec.TrustManagerConfig.TargetNamespaces)
	ns = append(ns, trustNamespace)
	slices.Sort(ns)
	return slices.Compact(ns)
}

func (r *Reconciler) createOrApplyTargetNamespaceRole(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string, namespace string) error {
	desired := getTargetNamespaceRoleObject(namespace, trustManager.Spec.TrustManagerConfig.SecretTargets, resourceLabels, resourceAnnotations)
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling target namespace role resource", "name", resourceName)

	existing := &rbacv1.Role{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if target namespace role %q exists", resourceName)
	}
	if exists && !roleModified(desired, existing) {
		r.log.V(4).Info("target namespace role resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("target namespace role resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply target namespace role %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "target namespace role resource %s applied", resourceName)
	return nil
}

func getTargetNamespaceRoleObject(namespace string, secretTargets v1alpha1.SecretTargetsConfig, resourceLabels, resourceAnnotations map[string]string) *rbacv1.Role {
	role := common.DecodeObjBytes[*rbacv1.Role](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(roleAssetName))
	common.UpdateName(role, trustManagerTargetRoleName)
	common.UpdateNamespace(role, namespace)
	common.UpdateResourceLabels(role, resourceLabels)
	updateResourceAnnotations(role, resourceAnnotations)
	role.Rules = namespacedTargetRules(secretTargets)
	return role
}

func (r *Reconciler) createOrApplyTargetNamespaceRoleBinding(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string, namespace string) error {
	desired := getTargetNamespaceRoleBindingObject(namespace, resourceLabels, resourceAnnotations)
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling target namespace rolebinding resource", "name", resourceName)

	existing := &rbacv1.RoleBinding{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if target namespace rolebinding %q exists", resourceName)
	}
	if exists && !roleBindingModified(desired, existing) {
		r.log.V(4).Info("target namespace rolebinding resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("target namespace rolebinding resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply target namespace rolebinding %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "target namespace rolebinding resource %s applied", resourceName)
	return nil
}

func getTargetNamespaceRoleBindingObject(namespace string, resourceLabels, resourceAnnotations map[string]string) *rbacv1.RoleBinding {
	roleBinding := common.DecodeObjBytes[*rbacv1.RoleBinding](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(roleBindingAssetName))
	common.UpdateName(roleBinding, trustManagerTargetRoleBindingName)
	common.UpdateNamespace(roleBinding, namespace)
	common.UpdateResourceLabels(roleBinding, resourceLabels)
	updateResourceAnnotations(roleBinding, resourceAnnotations)
	roleBinding.RoleRef.Name = trustManagerTargetRoleName
	updateBindingSubjects(roleBinding.Subjects, trustManagerServiceAccountName, operandNamespace)
	return roleBinding
}

// cleanupStaleTargetNamespaceRBAC deletes leftover trust-manager-target Roles
// and RoleBindings in namespaces that are no longer in the desired set.
// Shrinking targetNamespaces does not delete leftover target ConfigMaps/Secrets;
// leftover write permission would, so the Roles must go.
func (r *Reconciler) cleanupStaleTargetNamespaceRBAC(trustManager *v1alpha1.TrustManager, trustNamespace string) error {
	desired := make(map[string]struct{})
	for _, ns := range targetRBACNamespaces(trustManager, trustNamespace) {
		desired[ns] = struct{}{}
	}

	if err := r.deleteStaleTargetRoles(trustManager, desired); err != nil {
		return err
	}
	return r.deleteStaleTargetRoleBindings(trustManager, desired)
}

func (r *Reconciler) deleteStaleTargetRoles(trustManager *v1alpha1.TrustManager, desired map[string]struct{}) error {
	var roles rbacv1.RoleList
	if err := r.List(r.ctx, &roles, client.MatchingLabels{common.ManagedResourceLabelKey: RequestEnqueueLabelValue}); err != nil {
		return common.FromClientError(err, "failed to list target namespace roles")
	}
	for i := range roles.Items {
		role := &roles.Items[i]
		if role.Name != trustManagerTargetRoleName {
			continue
		}
		if _, keep := desired[role.Namespace]; keep {
			continue
		}
		if err := r.Delete(r.ctx, role); err != nil && !apierrors.IsNotFound(err) {
			return common.FromClientError(err, "failed to delete target namespace role %s/%s", role.Namespace, role.Name)
		}
		r.log.V(2).Info("deleted stale target namespace role", "namespace", role.Namespace, "name", role.Name)
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "stale target namespace role %s/%s deleted", role.Namespace, role.Name)
	}
	return nil
}

func (r *Reconciler) deleteStaleTargetRoleBindings(trustManager *v1alpha1.TrustManager, desired map[string]struct{}) error {
	var roleBindings rbacv1.RoleBindingList
	if err := r.List(r.ctx, &roleBindings, client.MatchingLabels{common.ManagedResourceLabelKey: RequestEnqueueLabelValue}); err != nil {
		return common.FromClientError(err, "failed to list target namespace rolebindings")
	}
	for i := range roleBindings.Items {
		rb := &roleBindings.Items[i]
		if rb.Name != trustManagerTargetRoleBindingName {
			continue
		}
		if _, keep := desired[rb.Namespace]; keep {
			continue
		}
		if err := r.Delete(r.ctx, rb); err != nil && !apierrors.IsNotFound(err) {
			return common.FromClientError(err, "failed to delete target namespace rolebinding %s/%s", rb.Namespace, rb.Name)
		}
		r.log.V(2).Info("deleted stale target namespace rolebinding", "namespace", rb.Namespace, "name", rb.Name)
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "stale target namespace rolebinding %s/%s deleted", rb.Namespace, rb.Name)
	}
	return nil
}

// updateBindingSubjects sets the ServiceAccount name and namespace on RBAC binding subjects.
func updateBindingSubjects(subjects []rbacv1.Subject, serviceAccountName, namespace string) {
	for i := range subjects {
		if subjects[i].Kind == roleBindingSubjectKind {
			subjects[i].Name = serviceAccountName
			subjects[i].Namespace = namespace
		}
	}
}

// clusterRoleModified compares only the fields we manage via SSA.
func clusterRoleModified(desired, existing *rbacv1.ClusterRole) bool {
	return managedMetadataModified(desired, existing) ||
		!reflect.DeepEqual(desired.Rules, existing.Rules)
}

// clusterRoleBindingModified compares only the fields we manage via SSA.
func clusterRoleBindingModified(desired, existing *rbacv1.ClusterRoleBinding) bool {
	return managedMetadataModified(desired, existing) ||
		!reflect.DeepEqual(desired.RoleRef, existing.RoleRef) ||
		!reflect.DeepEqual(desired.Subjects, existing.Subjects)
}

// roleModified compares only the fields we manage via SSA.
func roleModified(desired, existing *rbacv1.Role) bool {
	return managedMetadataModified(desired, existing) ||
		!reflect.DeepEqual(desired.Rules, existing.Rules)
}

// roleBindingModified compares only the fields we manage via SSA.
func roleBindingModified(desired, existing *rbacv1.RoleBinding) bool {
	return managedMetadataModified(desired, existing) ||
		!reflect.DeepEqual(desired.RoleRef, existing.RoleRef) ||
		!reflect.DeepEqual(desired.Subjects, existing.Subjects)
}
