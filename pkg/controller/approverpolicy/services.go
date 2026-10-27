package approverpolicy

import (
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func getServiceObjects(instance *v1alpha1.ApproverPolicyManager) []client.Object {
	var objects []client.Object
	for _, name := range []string{operandName, operandName + "-metrics"} {
		obj := common.DecodeObjBytes[*corev1.Service](codecs, corev1.SchemeGroupVersion, assets.MustAsset(assetName("service", name)))
		prepareResource(instance, obj)
		objects = append(objects, obj)
	}
	return objects
}

func serviceModified(desired, existing *corev1.Service) bool {
	return managedMetadataModified(desired, existing) ||
		desired.Spec.Type != existing.Spec.Type ||
		!maps.Equal(desired.Spec.Selector, existing.Spec.Selector) ||
		!equality.Semantic.DeepEqual(desired.Spec.Ports, existing.Spec.Ports)
}
