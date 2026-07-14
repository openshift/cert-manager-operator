package trustmanager

import (
	"context"
	"fmt"
	"maps"
	"reflect"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

func init() {
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := rbacv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := admissionregistrationv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := certmanagerv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
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
			//nolint:err113 // finalizer and namespacedName included for debugging
			return fmt.Errorf("failed to add finalizer %q on trustmanager.openshift.operator.io %q", finalizer, namespacedName)
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
			//nolint:err113 // finalizer and namespacedName included for debugging
			return fmt.Errorf("failed to remove finalizer %q from trustmanager.openshift.operator.io %q", finalizer, namespacedName)
		}

		if err := r.UpdateWithRetry(ctx, trustManager); err != nil {
			return fmt.Errorf("failed to remove finalizers on %q trustmanager.openshift.operator.io with %w", namespacedName, err)
		}
		return nil
	}

	return nil
}

func validateTrustManagerConfig(trustManager *v1alpha1.TrustManager) error {
	if reflect.ValueOf(trustManager.Spec.TrustManagerConfig).IsZero() {
		//nolint:err113 // user-facing validation error message
		return fmt.Errorf("spec.trustManagerConfig config cannot be empty")
	}

	if labels := trustManager.Spec.ControllerConfig.Labels; len(labels) > 0 {
		if err := common.ValidateLabelsConfig(labels, controllerConfigFieldPath); err != nil {
			return err
		}
	}
	if annotations := trustManager.Spec.ControllerConfig.Annotations; len(annotations) > 0 {
		if err := common.ValidateAnnotationsConfig(annotations, controllerConfigFieldPath); err != nil {
			return err
		}
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

// secretTargetsEnabled returns true when the secretTargets policy is Custom
// and at least one authorized secret is configured.
func secretTargetsEnabled(config v1alpha1.SecretTargetsConfig) bool {
	return config.Policy == v1alpha1.SecretTargetsPolicyCustom && len(config.AuthorizedSecrets) > 0
}

// defaultCAPackageEnabled returns true when the defaultCAPackage policy is Enabled.
func defaultCAPackageEnabled(config v1alpha1.DefaultCAPackageConfig) bool {
	return config.Policy == v1alpha1.DefaultCAPackagePolicyEnabled
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

// getResourceAnnotations returns the annotations to apply to resources.
// It merges user-specified annotations with any required annotations.
func getResourceAnnotations(trustManager *v1alpha1.TrustManager) map[string]string {
	annotations := make(map[string]string)
	if len(trustManager.Spec.ControllerConfig.Annotations) != 0 {
		maps.Copy(annotations, trustManager.Spec.ControllerConfig.Annotations)
	}
	return annotations
}

// updateResourceAnnotations merges user-provided annotations into the object's existing annotations.
// User-provided annotations take precedence over existing ones on key conflicts.
func updateResourceAnnotations(obj client.Object, annotations map[string]string) {
	if len(annotations) == 0 {
		return
	}
	existing := obj.GetAnnotations()
	if existing == nil {
		existing = make(map[string]string)
	}
	maps.Copy(existing, annotations)
	obj.SetAnnotations(existing)
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

// managedAnnotationsModified checks whether all annotations present in desired
// exist with matching values in existing. Extra annotations on existing are
// allowed and do not count as modified.
func managedAnnotationsModified(desired, existing client.Object) bool {
	existingAnnotations := existing.GetAnnotations()
	for k, v := range desired.GetAnnotations() {
		if existingAnnotations[k] != v {
			return true
		}
	}
	return false
}

// managedMetadataModified returns true if any managed label or annotation has drifted.
func managedMetadataModified(desired, existing client.Object) bool {
	return managedLabelsModified(desired, existing) || managedAnnotationsModified(desired, existing)
}

// namespaceExists checks if a namespace exists in the cluster.
func (r *Reconciler) namespaceExists(namespace string) (bool, error) {
	ns := &corev1.Namespace{}
	key := client.ObjectKey{Name: namespace}
	return r.Exists(r.ctx, key, ns)
}

// reconcileResourceWithSSA is a generic helper for reconciling Kubernetes resources using Server-Side Apply.
// It checks if the resource exists and has been modified, then applies changes if needed.
// The modifiedCheck function should compare desired vs existing and return true if different.
func (r *Reconciler) reconcileResourceWithSSA(
	trustManager *v1alpha1.TrustManager,
	desired, existing client.Object,
	resourceKind string,
	modifiedCheck func() bool,
) error {
	var resourceName string
	if desired.GetNamespace() != "" {
		resourceName = fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	} else {
		resourceName = desired.GetName()
	}

	r.log.V(4).Info("reconciling "+resourceKind+" resource", "name", resourceName)

	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if %s %q exists", resourceKind, resourceName)
	}
	if exists && !modifiedCheck() {
		r.log.V(4).Info(resourceKind+" resource exists and is in desired state", "name", resourceName)
		return nil
	}

	if !exists {
		r.log.V(2).Info("creating "+resourceKind+" resource", "name", resourceName)
	} else {
		r.log.V(2).Info("updating "+resourceKind+" resource", "name", resourceName)
	}
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply %s %q", resourceKind, resourceName)
	}

	if !exists {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "%s resource %s created", resourceKind, resourceName)
	} else {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "%s resource %s updated", resourceKind, resourceName)
	}
	return nil
}
