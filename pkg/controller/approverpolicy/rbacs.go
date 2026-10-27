package approverpolicy

import (
	"slices"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func getRBACObjects(instance *v1alpha1.ApproverPolicyManager) []client.Object {
	clusterRole := common.DecodeObjBytes[*rbacv1.ClusterRole](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(assetName("clusterrole", operandName)))
	for i := range clusterRole.Rules {
		rule := &clusterRole.Rules[i]
		if slices.Equal(rule.APIGroups, []string{"cert-manager.io"}) &&
			slices.Equal(rule.Resources, []string{"signers"}) && slices.Equal(rule.Verbs, []string{"approve"}) {
			rule.ResourceNames = slices.Clone(instance.Spec.ApproverPolicyConfig.ApproveSignerNames)
		}
	}
	objects := []client.Object{
		clusterRole,
		common.DecodeObjBytes[*rbacv1.ClusterRoleBinding](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(assetName("clusterrolebinding", operandName))),
		common.DecodeObjBytes[*rbacv1.Role](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(assetName("role", operandName))),
		common.DecodeObjBytes[*rbacv1.RoleBinding](codecs, rbacv1.SchemeGroupVersion, assets.MustAsset(assetName("rolebinding", operandName))),
	}
	for _, obj := range objects {
		prepareResource(instance, obj)
	}
	return objects
}

func rbacModified(desired, existing client.Object) bool {
	if managedMetadataModified(desired, existing) {
		return true
	}
	switch desired := desired.(type) {
	case *rbacv1.ClusterRole:
		return !equality.Semantic.DeepEqual(desired.Rules, existing.(*rbacv1.ClusterRole).Rules)
	case *rbacv1.ClusterRoleBinding:
		actual := existing.(*rbacv1.ClusterRoleBinding)
		return !equality.Semantic.DeepEqual(desired.RoleRef, actual.RoleRef) || !equality.Semantic.DeepEqual(desired.Subjects, actual.Subjects)
	case *rbacv1.Role:
		return !equality.Semantic.DeepEqual(desired.Rules, existing.(*rbacv1.Role).Rules)
	case *rbacv1.RoleBinding:
		actual := existing.(*rbacv1.RoleBinding)
		return !equality.Semantic.DeepEqual(desired.RoleRef, actual.RoleRef) || !equality.Semantic.DeepEqual(desired.Subjects, actual.Subjects)
	}
	return false
}
