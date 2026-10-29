package trustmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
		return fmt.Errorf("spec.trustManagerConfig config cannot be empty")
	}

	if labels := trustManager.Spec.ControllerConfig.Labels; len(labels) > 0 {
		if err := common.ValidateLabelsConfig(labels, controllerConfigFieldPath); err != nil {
			return err
		}
		if err := validateReservedKeys(labels, reservedLabelKeys, controllerConfigFieldPath, "labels"); err != nil {
			return err
		}
	}
	if annotations := trustManager.Spec.ControllerConfig.Annotations; len(annotations) > 0 {
		if err := common.ValidateAnnotationsConfig(annotations, controllerConfigFieldPath); err != nil {
			return err
		}
		if err := validateReservedKeys(annotations, reservedAnnotationKeys, controllerConfigFieldPath, "annotations"); err != nil {
			return err
		}
	}
	return nil
}

// validateReservedKeys checks that none of the user-configured keys conflict
// with controller-managed metadata keys.
func validateReservedKeys(userKeys map[string]string, reserved map[string]struct{}, fldPath *field.Path, fieldName string) error {
	var errs field.ErrorList
	for k := range userKeys {
		if _, ok := reserved[k]; ok {
			errs = append(errs, field.Forbidden(
				fldPath.Child(fieldName).Key(k),
				fmt.Sprintf("key %q is reserved for controller use and cannot be configured by the user", k),
			))
		}
	}
	return errs.ToAggregate()
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

// managedLabelsModified checks whether labels managed by this controller have
// drifted between the desired and existing objects. It detects both:
//   - Forward drift: a desired label is missing or has the wrong value on existing.
//   - Stale keys: a label previously owned by this controller (via SSA managedFields)
//     is no longer present in the desired state and needs pruning.
//
// Extra labels on existing that were never owned by this controller are ignored.
func managedLabelsModified(desired, existing client.Object) bool {
	existingLabels := existing.GetLabels()
	for k, v := range desired.GetLabels() {
		if existingLabels[k] != v {
			return true
		}
	}
	ownedLabels, _ := ownedMetadataKeys(existing, fieldOwner)
	if managedFieldsDeleted(ownedLabels, desired.GetLabels()) {
		return true
	}
	return false
}

// managedAnnotationsModified checks whether annotations managed by this controller
// have drifted between the desired and existing objects. It detects both forward
// drift and stale keys, mirroring the logic in managedLabelsModified.
func managedAnnotationsModified(desired, existing client.Object) bool {
	existingAnnotations := existing.GetAnnotations()
	for k, v := range desired.GetAnnotations() {
		if existingAnnotations[k] != v {
			return true
		}
	}
	_, ownedAnnotations := ownedMetadataKeys(existing, fieldOwner)
	if managedFieldsDeleted(ownedAnnotations, desired.GetAnnotations()) {
		return true
	}
	return false
}

// managedMetadataModified returns true if any managed label or annotation has drifted.
func managedMetadataModified(desired, existing client.Object) bool {
	return managedLabelsModified(desired, existing) || managedAnnotationsModified(desired, existing)
}

// ownedMetadataKeys extracts the label and annotation keys previously claimed
// by the given field manager from the object's managedFields entries.
// It only considers entries where Operation == Apply to match SSA ownership.
// If managedFields parsing fails, empty sets are returned (fail-open).
func ownedMetadataKeys(obj client.Object, manager string) (labels map[string]struct{}, annotations map[string]struct{}) {
	labels = make(map[string]struct{})
	annotations = make(map[string]struct{})

	for _, mf := range obj.GetManagedFields() {
		if mf.Manager != manager || mf.Operation != metav1.ManagedFieldsOperationApply {
			continue
		}
		if mf.FieldsV1 == nil {
			continue
		}
		ownedLabels, ownedAnnotations := parseFieldsV1Metadata(mf.FieldsV1.Raw)
		for k := range ownedLabels {
			labels[k] = struct{}{}
		}
		for k := range ownedAnnotations {
			annotations[k] = struct{}{}
		}
	}
	return labels, annotations
}

// fieldsV1Metadata is the minimal structure needed to extract label and
// annotation keys from the FieldsV1 JSON blob in managedFields.
// The JSON shape is: {"f:metadata": {"f:labels": {"f:<key>": {}}, "f:annotations": {"f:<key>": {}}}}
type fieldsV1Metadata struct {
	Metadata struct {
		Labels      map[string]json.RawMessage `json:"f:labels"`
		Annotations map[string]json.RawMessage `json:"f:annotations"`
	} `json:"f:metadata"`
}

// parseFieldsV1Metadata parses the FieldsV1 JSON and returns the label and
// annotation keys (with the "f:" prefix stripped). Returns empty maps on
// parse failure (fail-open).
func parseFieldsV1Metadata(raw []byte) (labels map[string]struct{}, annotations map[string]struct{}) {
	labels = make(map[string]struct{})
	annotations = make(map[string]struct{})

	if len(raw) == 0 {
		return labels, annotations
	}

	var fields fieldsV1Metadata
	if err := json.Unmarshal(raw, &fields); err != nil {
		return labels, annotations
	}

	for k := range fields.Metadata.Labels {
		if key, ok := strings.CutPrefix(k, "f:"); ok {
			labels[key] = struct{}{}
		}
	}
	for k := range fields.Metadata.Annotations {
		if key, ok := strings.CutPrefix(k, "f:"); ok {
			annotations[key] = struct{}{}
		}
	}
	return labels, annotations
}

// managedFieldsDeleted returns true if any key in ownedKeys is absent from
// desiredKeys, indicating that a previously owned field should be pruned.
func managedFieldsDeleted(ownedKeys map[string]struct{}, desiredKeys map[string]string) bool {
	for k := range ownedKeys {
		if _, exists := desiredKeys[k]; !exists {
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
