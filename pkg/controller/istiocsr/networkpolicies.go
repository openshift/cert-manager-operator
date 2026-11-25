package istiocsr

import (
	"fmt"
	"maps"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyNetworkPolicies(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) error {
	r.log.V(4).Info("reconciling istio-csr network policies", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())

	for _, assetPath := range istioCSRNetworkPolicyAssets {
		obj, err := r.getNetworkPolicyFromAsset(assetPath, istiocsr, resourceLabels)
		if err != nil {
			return fmt.Errorf("failed to get network policy from asset %s: %w", assetPath, err)
		}
		if err := common.ApplyResource(r.ctx, r.CtrlClient, r.log, r.eventRecorder, istiocsr, obj, &networkingv1.NetworkPolicy{}, fieldOwner,
			func(d, e *networkingv1.NetworkPolicy) bool { return hasObjectChanged(d, e) },
		); err != nil {
			return fmt.Errorf("failed to apply network policy from %s: %w", assetPath, err)
		}
	}

	return nil
}

func (r *Reconciler) getNetworkPolicyFromAsset(assetPath string, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) (*networkingv1.NetworkPolicy, error) {
	namespace := istiocsr.GetNamespace()
	if namespace == "" {
		namespace = istiocsr.Spec.IstioCSRConfig.Istio.Namespace
	}

	assetBytes := assets.MustAsset(assetPath)
	obj, err := runtime.Decode(codecs.UniversalDeserializer(), assetBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to decode network policy asset %s: %w", assetPath, err)
	}

	policy, ok := obj.(*networkingv1.NetworkPolicy)
	if !ok {
		return nil, fmt.Errorf("decoded object is not a NetworkPolicy, got %T", obj)
	}

	policy.Namespace = namespace

	if policy.Labels == nil {
		policy.Labels = make(map[string]string)
	}
	maps.Copy(policy.Labels, resourceLabels)

	return policy, nil
}
