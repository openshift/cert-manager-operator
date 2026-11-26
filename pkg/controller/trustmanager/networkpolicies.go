package trustmanager

import (
	"fmt"
	"maps"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyNetworkPolicies(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string) error {
	r.log.V(4).Info("reconciling trust-manager network policies", "name", trustManager.GetName())

	for _, assetPath := range trustManagerNetworkPolicyAssets {
		obj, err := getNetworkPolicyFromAsset(assetPath, resourceLabels)
		if err != nil {
			return fmt.Errorf("failed to get network policy from asset %s: %w", assetPath, err)
		}
		if err := r.createOrUpdateNetworkPolicy(trustManager, obj); err != nil {
			return fmt.Errorf("failed to create/update network policy from %s: %w", assetPath, err)
		}
	}

	return nil
}

func getNetworkPolicyFromAsset(assetPath string, resourceLabels map[string]string) (*networkingv1.NetworkPolicy, error) {
	policy := common.DecodeObjBytes[*networkingv1.NetworkPolicy](codecs, schema.GroupVersion{Group: networkingv1.GroupName, Version: "v1"}, assets.MustAsset(assetPath))
	common.UpdateNamespace(policy, operandNamespace)

	if policy.Labels == nil {
		policy.Labels = make(map[string]string)
	}
	maps.Copy(policy.Labels, resourceLabels)
	policy.Labels[trustManagerOwnedByLabelKey] = trustManagerOwnedByLabelValue

	return policy, nil
}

func (r *Reconciler) createOrUpdateNetworkPolicy(trustManager *v1alpha1.TrustManager, policy *networkingv1.NetworkPolicy) error {
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
		return common.FromClientError(err, "failed to check %s network policy resource already exists", policyName)
	}

	if exist {
		if networkPolicyChanged(desired, fetched) {
			r.log.V(1).Info("network policy has been modified, updating to desired state", "name", policyName)
			if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
				return common.FromClientError(err, "failed to update %s network policy resource", policyName)
			}
			r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "network policy resource %s reconciled back to desired state", policyName)
		} else {
			r.log.V(4).Info("network policy resource already exists and is in expected state", "name", policyName)
		}
	}

	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return common.FromClientError(err, "failed to create %s network policy resource", policyName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "network policy resource %s created", policyName)
	}

	return nil
}

func networkPolicyChanged(desired, fetched *networkingv1.NetworkPolicy) bool {
	if managedMetadataModified(desired, fetched) {
		return true
	}
	return !reflect.DeepEqual(desired.Spec, fetched.Spec)
}

// Expose spec comparison for unit tests.
func networkPolicySpecEqual(desired, fetched *networkingv1.NetworkPolicy) bool {
	return reflect.DeepEqual(desired.Spec, fetched.Spec)
}