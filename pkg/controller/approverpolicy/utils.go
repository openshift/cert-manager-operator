package approverpolicy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

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
