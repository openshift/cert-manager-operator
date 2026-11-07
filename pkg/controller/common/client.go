package common

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// ctrlClientImpl implements the CtrlClient interface using the manager's client.
type ctrlClientImpl struct {
	client.Client
}

// CtrlClient defines the interface for controller client operations.
//
//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate
//counterfeiter:generate -o fakes . CtrlClient
type CtrlClient interface {
	Get(context.Context, client.ObjectKey, client.Object) error
	List(context.Context, client.ObjectList, ...client.ListOption) error
	StatusUpdate(context.Context, client.Object, ...client.SubResourceUpdateOption) error
	Update(context.Context, client.Object, ...client.UpdateOption) error
	UpdateWithRetry(context.Context, client.Object, ...client.UpdateOption) error
	Create(context.Context, client.Object, ...client.CreateOption) error
	Delete(context.Context, client.Object, ...client.DeleteOption) error
	Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error
	Exists(context.Context, client.ObjectKey, client.Object) (bool, error)
}

// NewClient creates a new controller client from the manager.
// Use the manager's client directly instead of creating a custom client.
// The manager's client uses the manager's cache, which ensures the reconciler
// reads from the same cache that the controller's watches use, preventing
// cache mismatch issues.
func NewClient(m manager.Manager) (CtrlClient, error) {
	return &ctrlClientImpl{
		Client: m.GetClient(),
	}, nil
}

func (c *ctrlClientImpl) Get(
	ctx context.Context, key client.ObjectKey, obj client.Object,
) error {
	if err := c.Client.Get(ctx, key, obj); err != nil {
		return fmt.Errorf("failed to get object %q: %w", key, err)
	}
	return nil
}

func (c *ctrlClientImpl) List(
	ctx context.Context, list client.ObjectList, opts ...client.ListOption,
) error {
	if err := c.Client.List(ctx, list, opts...); err != nil {
		return fmt.Errorf("failed to list objects: %w", err)
	}
	return nil
}

func (c *ctrlClientImpl) Create(
	ctx context.Context, obj client.Object, opts ...client.CreateOption,
) error {
	key := client.ObjectKeyFromObject(obj)
	if err := c.Client.Create(ctx, obj, opts...); err != nil {
		return fmt.Errorf("failed to create object %q: %w", key, err)
	}
	return nil
}

func (c *ctrlClientImpl) Delete(
	ctx context.Context, obj client.Object, opts ...client.DeleteOption,
) error {
	key := client.ObjectKeyFromObject(obj)
	if err := c.Client.Delete(ctx, obj, opts...); err != nil {
		return fmt.Errorf("failed to delete object %q: %w", key, err)
	}
	return nil
}

func (c *ctrlClientImpl) Update(
	ctx context.Context, obj client.Object, opts ...client.UpdateOption,
) error {
	key := client.ObjectKeyFromObject(obj)
	if err := c.Client.Update(ctx, obj, opts...); err != nil {
		return fmt.Errorf("failed to update object %q: %w", key, err)
	}
	return nil
}

func (c *ctrlClientImpl) UpdateWithRetry(
	ctx context.Context, obj client.Object, opts ...client.UpdateOption,
) error {
	key := client.ObjectKeyFromObject(obj)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentInterface := reflect.New(reflect.TypeOf(obj).Elem()).Interface()
		current, ok := currentInterface.(client.Object)
		if !ok {
			return fmt.Errorf("failed to convert to client.Object")
		}
		if err := c.Client.Get(ctx, key, current); err != nil {
			return fmt.Errorf("failed to fetch latest %q for update: %w", key, err)
		}
		obj.SetResourceVersion(current.GetResourceVersion())
		if err := c.Client.Update(ctx, obj, opts...); err != nil {
			return fmt.Errorf("failed to update %q resource: %w", key, err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (c *ctrlClientImpl) StatusUpdate(
	ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption,
) error {
	key := client.ObjectKeyFromObject(obj)
	if err := c.Client.Status().Update(ctx, obj, opts...); err != nil {
		return fmt.Errorf("failed to update status for object %q: %w", key, err)
	}
	return nil
}

func (c *ctrlClientImpl) Patch(
	ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption,
) error {
	key := client.ObjectKeyFromObject(obj)
	if err := c.Client.Patch(ctx, obj, patch, opts...); err != nil {
		return fmt.Errorf("failed to patch object %q: %w", key, err)
	}
	return nil
}

func (c *ctrlClientImpl) Exists(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
	if err := c.Client.Get(ctx, key, obj); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// UpdateFinalizer adds or removes a finalizer, re-reading the object and applying
// only that change on each conflict retry. It rejects replacement objects and
// skips adding a finalizer once deletion has started.
// It leaves instance unchanged and returns the observed object on success, so
// callers can use its updated spec, metadata, and deletion timestamp.
func UpdateFinalizer[T client.Object](ctx context.Context, c CtrlClient, instance T, resource schema.GroupResource, finalizer string, add bool) (T, error) {
	if controllerutil.ContainsFinalizer(instance, finalizer) == add {
		return instance, nil
	}
	key := client.ObjectKeyFromObject(instance)
	updated := instance
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, ok := reflect.New(reflect.TypeOf(instance).Elem()).Interface().(T)
		if !ok {
			return fmt.Errorf("failed to create finalizer update object for %T", instance)
		}
		if err := c.Get(ctx, key, current); err != nil {
			return err
		}
		if current.GetUID() != instance.GetUID() {
			return errors.NewConflict(resource, key.Name, fmt.Errorf("object was replaced during reconciliation"))
		}
		if add && !current.GetDeletionTimestamp().IsZero() {
			updated = current
			return nil
		}
		var changed bool
		if add {
			changed = controllerutil.AddFinalizer(current, finalizer)
		} else {
			changed = controllerutil.RemoveFinalizer(current, finalizer)
		}
		if changed {
			if err := c.Update(ctx, current); err != nil {
				return err
			}
		}
		updated = current
		return nil
	})
	return updated, err
}
