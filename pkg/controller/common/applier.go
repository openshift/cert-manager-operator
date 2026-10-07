package common

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ApplyResource reconciles a desired Kubernetes object using Server-Side Apply.
// It checks whether the resource already exists and whether managed fields have
// drifted. If no drift is detected it returns early. Otherwise it patches the
// object with SSA using the given fieldOwner.
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
		return FromClientError(err, "failed to apply %s %q", kind, key)
	}

	recorder.Eventf(owner, corev1.EventTypeNormal, "Reconciled", "%s %s applied", kind, key)
	return nil
}
