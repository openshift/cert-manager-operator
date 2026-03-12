package trustmanager

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

var (
	errAddTMFinalizerFailed    = errors.New("failed to add finalizer on trustmanager.openshift.operator.io object")
	errRemoveTMFinalizerFailed = errors.New("failed to remove finalizer from trustmanager.openshift.operator.io object")
	errTrustManagerConfigEmpty = errors.New("spec.trustManagerConfig config cannot be empty")
)

func init() {
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	// TODO: Add more groups to scheme as resources are implemented
}

// updateStatus is for updating the status subresource of trustmanager.openshift.operator.io.
func (r *Reconciler) updateStatus(ctx context.Context, changed *v1alpha1.TrustManager) error {
	namespacedName := client.ObjectKeyFromObject(changed)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		r.log.V(4).Info("updating trustmanager.openshift.operator.io status", "request", namespacedName)
		current := &v1alpha1.TrustManager{}
		if err := r.Get(ctx, namespacedName, current); err != nil {
			return fmt.Errorf("failed to fetch trustmanager.openshift.operator.io %q for status update: %w", namespacedName, err)
		}
		changed.Status.DeepCopyInto(&current.Status)

		if err := r.StatusUpdate(ctx, current); err != nil {
			return fmt.Errorf("failed to update trustmanager.openshift.operator.io %q status: %w", namespacedName, err)
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

// addFinalizer adds finalizer to trustmanager.openshift.operator.io resource.
func (r *Reconciler) addFinalizer(ctx context.Context, trustManager *v1alpha1.TrustManager) error {
	namespacedName := client.ObjectKeyFromObject(trustManager)
	if !controllerutil.ContainsFinalizer(trustManager, finalizer) {
		if !controllerutil.AddFinalizer(trustManager, finalizer) {
			return fmt.Errorf("finalizer %q on %q: %w", finalizer, namespacedName, errAddTMFinalizerFailed)
		}

		// update trustmanager.openshift.operator.io on adding finalizer.
		if err := r.UpdateWithRetry(ctx, trustManager); err != nil {
			return fmt.Errorf("failed to add finalizers on %q trustmanager.openshift.operator.io with %w", namespacedName, err)
		}

		updated := &v1alpha1.TrustManager{}
		if err := r.Get(ctx, namespacedName, updated); err != nil {
			return fmt.Errorf("failed to fetch trustmanager.openshift.operator.io %q after updating finalizers: %w", namespacedName, err)
		}
		updated.DeepCopyInto(trustManager)
		return nil
	}
	return nil
}

// removeFinalizer removes finalizers added to trustmanager.openshift.operator.io resource.
func (r *Reconciler) removeFinalizer(ctx context.Context, trustManager *v1alpha1.TrustManager, finalizer string) error {
	namespacedName := client.ObjectKeyFromObject(trustManager)
	if controllerutil.ContainsFinalizer(trustManager, finalizer) {
		if !controllerutil.RemoveFinalizer(trustManager, finalizer) {
			return fmt.Errorf("finalizer %q on %q: %w", finalizer, namespacedName, errRemoveTMFinalizerFailed)
		}

		if err := r.UpdateWithRetry(ctx, trustManager); err != nil {
			return fmt.Errorf("failed to remove finalizers on %q trustmanager.openshift.operator.io with %w", namespacedName, err)
		}
		return nil
	}

	return nil
}

func containsProcessedAnnotation(trustManager *v1alpha1.TrustManager) bool {
	return common.ContainsAnnotation(trustManager, controllerProcessedAnnotation)
}

func addProcessedAnnotation(trustManager *v1alpha1.TrustManager) bool {
	return common.AddAnnotation(trustManager, controllerProcessedAnnotation, "true")
}

func decodeServiceAccountObjBytes(objBytes []byte) *corev1.ServiceAccount {
	obj, err := runtime.Decode(codecs.UniversalDecoder(corev1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*corev1.ServiceAccount)
}

func validateTrustManagerConfig(trustManager *v1alpha1.TrustManager) error {
	if reflect.ValueOf(trustManager.Spec.TrustManagerConfig).IsZero() {
		return errTrustManagerConfigEmpty
	}
	return nil
}

func (r *Reconciler) updateCondition(trustManager *v1alpha1.TrustManager, prependErr error) error {
	if err := r.updateStatus(r.ctx, trustManager); err != nil {
		errUpdate := fmt.Errorf("failed to update %s status: %w", trustManager.GetName(), err)
		if prependErr != nil {
			return utilerrors.NewAggregate([]error{prependErr, errUpdate})
		}
		return errUpdate
	}
	return prependErr
}

// getTrustNamespace returns the trust namespace from the TrustManager config.
// If not specified, returns the default trust namespace.
func getTrustNamespace(trustManager *v1alpha1.TrustManager) string {
	if trustManager.Spec.TrustManagerConfig.TrustNamespace != "" {
		return trustManager.Spec.TrustManagerConfig.TrustNamespace
	}
	return defaultTrustNamespace
}

// getResourceLabels returns the labels to apply to all resources created by the controller.
// It merges user-specified labels with the controller's default labels.
func getResourceLabels(trustManager *v1alpha1.TrustManager) map[string]string {
	resourceLabels := make(map[string]string)
	if len(trustManager.Spec.ControllerConfig.Labels) != 0 {
		maps.Copy(resourceLabels, trustManager.Spec.ControllerConfig.Labels)
	}
	maps.Copy(resourceLabels, controllerDefaultResourceLabels)
	return resourceLabels
}

// managedLabelsModified checks whether all labels present in desired exist
// with matching values in existing. Extra labels on existing (added by users
// or other controllers) are allowed and do not count as modified.
func managedLabelsModified(desired, existing client.Object) bool {
	existingLabels := existing.GetLabels()
	for k, v := range desired.GetLabels() {
		if existingLabels[k] != v {
			return true
		}
	}
	return false
}

// namespaceExists checks if a namespace exists in the cluster.
func (r *Reconciler) namespaceExists(namespace string) (bool, error) {
	ns := &corev1.Namespace{}
	key := client.ObjectKey{Name: namespace}
	return r.Exists(r.ctx, key, ns)
}
