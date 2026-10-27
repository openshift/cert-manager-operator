package http01proxy

import (
	"context"
	"fmt"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func (r *Reconciler) updateStatus(ctx context.Context, changed *v1alpha1.HTTP01Proxy) error {
	namespacedName := client.ObjectKeyFromObject(changed)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		r.log.V(4).Info("updating http01proxy status", "request", namespacedName)
		current := &v1alpha1.HTTP01Proxy{}
		if err := r.Get(ctx, namespacedName, current); err != nil {
			return fmt.Errorf("failed to fetch http01proxy %q for status update: %w", namespacedName, err)
		}
		changed.Status.DeepCopyInto(&current.Status)
		if err := r.StatusUpdate(ctx, current); err != nil {
			return fmt.Errorf("failed to update http01proxy %q status: %w", namespacedName, err)
		}
		return nil
	})
}

func (r *Reconciler) addFinalizer(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	namespacedName := client.ObjectKeyFromObject(proxy)
	if !controllerutil.ContainsFinalizer(proxy, finalizer) {
		if !controllerutil.AddFinalizer(proxy, finalizer) {
			return fmt.Errorf("failed to create %q http01proxy object with finalizers added", namespacedName)
		}
		if err := r.UpdateWithRetry(ctx, proxy); err != nil {
			return fmt.Errorf("failed to add finalizers on %q http01proxy with %w", namespacedName, err)
		}
		updated := &v1alpha1.HTTP01Proxy{}
		if err := r.Get(ctx, namespacedName, updated); err != nil {
			return fmt.Errorf("failed to fetch http01proxy %q after updating finalizers: %w", namespacedName, err)
		}
		updated.DeepCopyInto(proxy)
	}
	return nil
}

func (r *Reconciler) removeFinalizer(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	namespacedName := client.ObjectKeyFromObject(proxy)
	if controllerutil.ContainsFinalizer(proxy, finalizer) {
		if !controllerutil.RemoveFinalizer(proxy, finalizer) {
			return fmt.Errorf("failed to create %q http01proxy object with finalizers removed", namespacedName)
		}
		if err := r.UpdateWithRetry(ctx, proxy); err != nil {
			return fmt.Errorf("failed to remove finalizers on %q http01proxy with %w", namespacedName, err)
		}
	}
	return nil
}

func (r *Reconciler) updateCondition(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, prependErr error) error {
	if err := r.updateStatus(ctx, proxy); err != nil {
		errUpdate := fmt.Errorf("failed to update %s/%s status: %w", proxy.GetNamespace(), proxy.GetName(), err)
		if prependErr != nil {
			return utilerrors.NewAggregate([]error{prependErr, errUpdate})
		}
		return errUpdate
	}
	return prependErr
}
