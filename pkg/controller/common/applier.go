package common

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyResource reconciles a desired Kubernetes object to its desired state.
// It checks whether the resource already exists and whether managed fields have
// drifted. If no drift is detected it returns early.
//
// Both creation and drift correction use Server-Side Apply with the given
// fieldOwner, forcing ownership. SSA preserves fields managed by other
// controllers (e.g. the CA bundle a CA injector writes into a webhook
// configuration) and prunes fields this manager previously applied but no
// longer desires (e.g. a pod-template annotation tied to a disabled feature).
//
// SSA cannot, however, correct drift in the key fields of a granular list-map
// entry (e.g. Service spec.ports, keyed by port and protocol): the applied
// entry is added alongside the drifted one and fails validation with duplicate
// list keys. When a drift-correction apply fails, ApplyResource falls back to
// a JSON merge patch, which replaces lists wholesale and restores the desired
// entry.
//
// T is the concrete object type (e.g. *corev1.Service). The hasChanged callback
// receives typed desired and existing objects so callers can compare fields
// without type assertions.
func ApplyResource[T client.Object](
	ctx context.Context,
	c CtrlClient,
	log logr.Logger,
	recorder record.EventRecorder,
	owner client.Object,
	desired T,
	existing T,
	fieldOwner string,
	hasChanged func(desired, existing T) bool,
) error {
	key := client.ObjectKeyFromObject(desired)
	kind := reflect.TypeOf(desired).Elem().Name()

	log.V(4).Info("reconciling resource", "kind", kind, "name", key)

	exists, err := c.Exists(ctx, key, existing)
	if err != nil {
		return FromClientError(err, "failed to check if %s %q exists", kind, key)
	}
	if exists && !hasChanged(desired, existing) {
		log.V(4).Info("resource exists and is in desired state", "kind", kind, "name", key)
		return nil
	}

	log.V(2).Info("applying resource to desired state", "kind", kind, "name", key)
	if err := c.Patch(ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		if !exists {
			return FromClientError(err, "failed to apply %s %q", kind, key)
		}
		// SSA cannot correct drift in the key fields of a granular list-map
		// entry (e.g. Service spec.ports, keyed by port and protocol): the
		// applied entry is added alongside the drifted one and fails
		// validation with duplicate list keys. Fall back to a JSON merge
		// patch, which replaces lists wholesale and restores the desired
		// entry.
		if mergeErr := c.Patch(ctx, desired, client.Merge, client.FieldOwner(fieldOwner)); mergeErr != nil {
			return FromClientError(err, "failed to apply %s %q", kind, key)
		}
	}

	recorder.Eventf(owner, corev1.EventTypeNormal, "Reconciled", "%s %s applied", kind, key)
	return nil
}
