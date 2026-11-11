package approverpolicy

import (
	"context"
	"slices"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyRBACResources(ctx context.Context, instance *v1alpha1.ApproverPolicyManager) error {
	for _, desired := range getRBACObjects(instance) {
		if err := r.applyResource(ctx, instance, desired); err != nil {
			return err
		}
	}
	return nil
}

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
