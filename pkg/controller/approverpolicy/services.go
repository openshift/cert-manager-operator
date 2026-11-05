package approverpolicy

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServices(ctx context.Context, instance *v1alpha1.ApproverPolicyManager) error {
	for _, desired := range getServiceObjects(instance) {
		if err := r.applyResource(ctx, instance, desired); err != nil {
			return err
		}
	}
	return nil
}

func getServiceObjects(instance *v1alpha1.ApproverPolicyManager) []client.Object {
	var objects []client.Object
	for _, name := range []string{operandName, operandName + "-metrics"} {
		obj := common.DecodeObjBytes[*corev1.Service](codecs, corev1.SchemeGroupVersion, assets.MustAsset(assetName("service", name)))
		prepareResource(instance, obj)
		objects = append(objects, obj)
	}
	return objects
}
