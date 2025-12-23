package istiocsr

import (
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyNetworkPolicies(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	r.log.V(4).Info("reconciling istio-csr network policies", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())

	// Apply static network policy assets for istio-csr
	for _, assetPath := range istioCSRNetworkPolicyAssets {
		obj, err := r.getNetworkPolicyFromAsset(assetPath, istiocsr, resourceLabels)
		if err != nil {
			return fmt.Errorf("failed to get network policy from asset %s: %w", assetPath, err)
		}
		if err := r.createOrUpdateNetworkPolicy(obj, istioCSRCreateRecon); err != nil {
			return fmt.Errorf("failed to create/update network policy from %s: %w", assetPath, err)
		}
	}

	return nil
}

func (r *Reconciler) getNetworkPolicyFromAsset(assetPath string, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) (*networkingv1.NetworkPolicy, error) {
	// Get the target namespace for istio-csr deployment
	namespace := istiocsr.GetNamespace()
	if namespace == "" {
		namespace = istiocsr.Spec.IstioCSRConfig.Istio.Namespace
	}

	// Read the asset and decode it
	assetBytes := assets.MustAsset(assetPath)
	obj, err := runtime.Decode(codecs.UniversalDeserializer(), assetBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to decode network policy asset %s: %w", assetPath, err)
	}

	policy, ok := obj.(*networkingv1.NetworkPolicy)
	if !ok {
		return nil, fmt.Errorf("decoded object is not a NetworkPolicy, got %T", obj)
	}

	// Set the correct namespace
	policy.Namespace = namespace

	// Merge resource labels
	if policy.Labels == nil {
		policy.Labels = make(map[string]string)
	}
	maps.Copy(policy.Labels, resourceLabels)

	return policy, nil
}

func (r *Reconciler) createOrUpdateNetworkPolicy(policy *networkingv1.NetworkPolicy, istioCSRCreateRecon bool) error {
	desired := policy.DeepCopy()
	policyName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling network policy resource", "name", policyName)

	fetched := &networkingv1.NetworkPolicy{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s network policy resource already exists", policyName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(policy, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s network policy resource already exists, maybe from previous installation", policyName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("network policy has been modified, updating to desired state", "name", policyName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s network policy resource", policyName)
		}
		r.eventRecorder.Eventf(policy, corev1.EventTypeNormal, "Reconciled", "network policy resource %s reconciled back to desired state", policyName)
	} else {
		r.log.V(4).Info("network policy resource already exists and is in expected state", "name", policyName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s network policy resource", policyName)
		}
		r.eventRecorder.Eventf(policy, corev1.EventTypeNormal, "Reconciled", "network policy resource %s created", policyName)
	}

	return nil
}
