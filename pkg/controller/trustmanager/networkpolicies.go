package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyNetworkPolicies(tm *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	for _, assetPath := range trustManagerNetworkPolicyAssets {
		if err := r.createOrApplyNetworkPolicy(tm, assetPath, resourceLabels, trustManagerCreateRecon); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) createOrApplyNetworkPolicy(tm *v1alpha1.TrustManager, assetPath string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	assetBytes := assets.MustAsset(assetPath)
	desired := decodeNetworkPolicyObjBytes(assetBytes)
	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}
	updateNamespace(desired, trustNamespace)
	updateResourceLabels(desired, resourceLabels)

	policyName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling network policy resource", "name", policyName)

	fetched := &networkingv1.NetworkPolicy{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s network policy resource already exists", policyName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s network policy resource already exists, maybe from previous installation", policyName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("network policy has been modified, updating to desired state", "name", policyName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s network policy resource", policyName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "network policy resource %s reconciled back to desired state", policyName)
	} else {
		r.log.V(4).Info("network policy resource already exists and is in expected state", "name", policyName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s network policy resource", policyName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "network policy resource %s created", policyName)
	}

	return nil
}
