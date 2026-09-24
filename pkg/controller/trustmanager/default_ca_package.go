package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// createOrApplyDefaultCAPackage creates and manages the default CA package ConfigMap
// using OpenShift's CNO trusted CA bundle injection mechanism.
//
// When DefaultCAPackage policy is "Enabled", a ConfigMap is created with the
// "config.openshift.io/inject-trusted-cabundle" annotation. The Cluster Network Operator (CNO)
// automatically injects the cluster's trusted CA bundle into this ConfigMap, which trust-manager
// then uses as the default CA package for Bundles that specify "useDefaultCAs: true".
func (r *Reconciler) createOrApplyDefaultCAPackage(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getDefaultCAPackageConfigMap(resourceLabels)

	configMapName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling default CA package configmap resource", "name", configMapName)
	fetched := &corev1.ConfigMap{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s default CA package configmap resource already exists", configMapName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s default CA package configmap resource already exists, maybe from previous installation", configMapName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("default CA package configmap has been modified, updating to desired state", "name", configMapName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s default CA package configmap resource", configMapName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "default CA package configmap resource %s reconciled back to desired state", configMapName)
	} else {
		r.log.V(4).Info("default CA package configmap resource already exists and is in expected state", "name", configMapName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s default CA package configmap resource", configMapName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "default CA package configmap resource %s created", configMapName)
	}

	return nil
}

func (r *Reconciler) getDefaultCAPackageConfigMap(resourceLabels map[string]string) *corev1.ConfigMap {
	// Merge resource labels with CNO trusted CA bundle injection annotation
	annotations := map[string]string{
		cnoTrustedCAAnnotation: "true",
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        defaultCAPackageConfigMapName,
			Namespace:   trustManagerOperandNamespace,
			Labels:      resourceLabels,
			Annotations: annotations,
		},
		// Data is intentionally left empty. The CNO trusted CA bundle injector
		// will populate the ca-certificates.crt key with the cluster's trusted CA bundle.
		Data: map[string]string{},
	}

	return configMap
}
