package approverpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

var codecs = serializer.NewCodecFactory(clientscheme.Scheme)

func assetName(kind, name string) string {
	return "approver-policy/resources/" + kind + "_" + name + ".yml"
}

func prepareResource(instance *v1alpha1.ApproverPolicyManager, obj client.Object) {
	resourceLabels := maps.Clone(instance.Spec.ControllerConfig.Labels)
	if resourceLabels == nil {
		resourceLabels = map[string]string{}
	}
	maps.Copy(resourceLabels, obj.GetLabels())
	resourceLabels[common.ManagedResourceLabelKey] = RequestEnqueueLabelValue
	if version := os.Getenv(versionEnvVar); version != "" {
		resourceLabels["app.kubernetes.io/version"] = version
	}
	obj.SetLabels(resourceLabels)
	annotations := maps.Clone(instance.Spec.ControllerConfig.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	// Required injection annotations from the asset override user annotations.
	maps.Copy(annotations, obj.GetAnnotations())
	obj.SetAnnotations(annotations)
	obj.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(instance, v1alpha1.SchemeGroupVersion.WithKind("ApproverPolicyManager"))})
}

func managedMetadataModified(desired, existing client.Object) bool {
	return managedMapModified(desired.GetLabels(), existing.GetLabels(), existing, "f:metadata", "f:labels") ||
		managedMapModified(desired.GetAnnotations(), existing.GetAnnotations(), existing, "f:metadata", "f:annotations") ||
		!equality.Semantic.DeepEqual(metav1.GetControllerOf(desired), metav1.GetControllerOf(existing))
}

// Compare desired keys and removals of keys previously owned by our SSA manager.
// Extra keys from other writers do not constitute drift.
func managedMapModified(desired, existing map[string]string, obj client.Object, path ...string) bool {
	for key, value := range desired {
		if actual, ok := existing[key]; !ok || actual != value {
			return true
		}
	}
	for _, entry := range obj.GetManagedFields() {
		if entry.Manager != fieldOwner || entry.Subresource != "" || entry.FieldsV1 == nil {
			continue
		}
		var fields map[string]interface{}
		if json.Unmarshal(entry.FieldsV1.Raw, &fields) != nil {
			return true
		}
		for _, segment := range path {
			fields, _ = fields[segment].(map[string]interface{})
		}
		for key := range fields {
			if !strings.HasPrefix(key, "f:") {
				continue
			}
			key = strings.TrimPrefix(key, "f:")
			if _, wanted := desired[key]; !wanted {
				if _, present := existing[key]; present {
					return true
				}
			}
		}
	}
	return false
}

func (r *Reconciler) updateFinalizer(ctx context.Context, instance *v1alpha1.ApproverPolicyManager, add bool) error {
	if controllerutil.ContainsFinalizer(instance, finalizer) == add {
		return nil
	}
	key := client.ObjectKeyFromObject(instance)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &v1alpha1.ApproverPolicyManager{}
		if err := r.Get(ctx, key, current); err != nil {
			return err
		}
		if current.UID != instance.UID {
			return apierrors.NewConflict(v1alpha1.Resource("approverpolicymanagers"), key.Name,
				fmt.Errorf("ApproverPolicyManager was replaced during reconciliation"))
		}
		if add && !current.DeletionTimestamp.IsZero() {
			current.DeepCopyInto(instance)
			return nil
		}
		var changed bool
		if add {
			changed = controllerutil.AddFinalizer(current, finalizer)
		} else {
			changed = controllerutil.RemoveFinalizer(current, finalizer)
		}
		if changed {
			if err := r.Update(ctx, current); err != nil {
				return err
			}
			if add {
				r.log.V(1).Info("added finalizer", "name", current.Name)
			} else {
				r.log.V(1).Info("removed finalizer", "name", current.Name)
			}
		}
		current.DeepCopyInto(instance)
		return nil
	})
}

func (r *Reconciler) updateStatus(ctx context.Context, instance *v1alpha1.ApproverPolicyManager, desired v1alpha1.ConditionalStatus) error {
	key := client.ObjectKeyFromObject(instance)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &v1alpha1.ApproverPolicyManager{}
		if err := r.Get(ctx, key, current); err != nil {
			return fmt.Errorf("failed to fetch ApproverPolicyManager status: %w", err)
		}
		if current.Generation != instance.Generation || current.UID != instance.UID {
			return apierrors.NewConflict(v1alpha1.Resource("approverpolicymanagers"), key.Name,
				fmt.Errorf("ApproverPolicyManager changed during reconciliation"))
		}
		previous := current.Status.DeepCopy()
		current.Status.ApproverPolicyImage = instance.Status.ApproverPolicyImage
		for _, condition := range desired.Conditions {
			condition.ObservedGeneration = instance.Generation
			meta.SetStatusCondition(&current.Status.Conditions, condition)
		}
		if equality.Semantic.DeepEqual(*previous, current.Status) {
			return nil
		}
		if err := r.StatusUpdate(ctx, current); err != nil {
			return fmt.Errorf("failed to update ApproverPolicyManager status: %w", err)
		}
		r.log.V(2).Info("updated status conditions", "name", current.Name, "conditions", current.Status.Conditions)
		return nil
	})
}

func (r *Reconciler) recordEvent(instance *v1alpha1.ApproverPolicyManager, reason, message string, args ...interface{}) {
	if r.eventRecorder != nil {
		r.eventRecorder.Eventf(instance, corev1.EventTypeNormal, reason, message, args...)
	}
}
