package trustmanager

import (
	"fmt"
	"maps"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyNetworkPolicies(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string) error {
	for _, assetPath := range trustManagerNetworkPolicyAssets {
		desired, err := r.getNetworkPolicyFromAsset(assetPath, resourceLabels)
		if err != nil {
			return fmt.Errorf("failed to load network policy asset %s: %w", assetPath, err)
		}
		if err := r.createOrApplyNetworkPolicy(trustManager, desired); err != nil {
			return fmt.Errorf("failed to reconcile network policy %s/%s: %w", desired.GetNamespace(), desired.GetName(), err)
		}
	}
	return nil
}

func (r *Reconciler) getNetworkPolicyFromAsset(assetPath string, resourceLabels map[string]string) (*networkingv1.NetworkPolicy, error) {
	policy := common.DecodeObjBytes[*networkingv1.NetworkPolicy](codecs, networkingv1.SchemeGroupVersion, assets.MustAsset(assetPath))
	common.UpdateNamespace(policy, operandNamespace)
	if policy.Labels == nil {
		policy.Labels = make(map[string]string)
	}
	maps.Copy(policy.Labels, resourceLabels)
	policy.Labels[trustManagerNetworkPolicyOwnedByLabelKey] = trustManagerNetworkPolicyOwnedByValue
	return policy, nil
}

func (r *Reconciler) createOrApplyNetworkPolicy(trustManager *v1alpha1.TrustManager, desired *networkingv1.NetworkPolicy) error {
	policyName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling network policy resource", "name", policyName)

	existing := &networkingv1.NetworkPolicy{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if network policy %q exists", policyName)
	}
	if exists && !networkPolicyModified(desired, existing) {
		r.log.V(4).Info("network policy resource exists and is in desired state", "name", policyName)
		return nil
	}

	r.log.V(2).Info("network policy resource has been modified, updating to desired state", "name", policyName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply network policy %q", policyName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "network policy resource %s applied", policyName)
	return nil
}

func networkPolicyModified(desired, existing *networkingv1.NetworkPolicy) bool {
	if managedMetadataModified(desired, existing) {
		return true
	}
	return !reflect.DeepEqual(desired.Spec, existing.Spec)
}
