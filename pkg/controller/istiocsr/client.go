package istiocsr

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type ctrlClientImpl struct {
	client.Client
}

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate
//counterfeiter:generate -o fakes . ctrlClient
type ctrlClient interface {
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

func NewClient(m manager.Manager) (ctrlClient, error) {
	// Use the manager's client directly instead of creating a custom client.
	// The manager's client uses the manager's cache, which ensures the reconciler
	// reads from the same cache that the controller's watches use, preventing
	// cache mismatch issues.
	return &ctrlClientImpl{
		Client: m.GetClient(),
	}, nil
}

func (c *ctrlClientImpl) Get(
	ctx context.Context, key client.ObjectKey, obj client.Object,
) error {
	return c.Client.Get(ctx, key, obj) //nolint:wrapcheck // error from client is already contextual
}

func (c *ctrlClientImpl) List(
	ctx context.Context, list client.ObjectList, opts ...client.ListOption,
) error {
	return c.Client.List(ctx, list, opts...) //nolint:wrapcheck // error from client is already contextual
}

func (c *ctrlClientImpl) Create(
	ctx context.Context, obj client.Object, opts ...client.CreateOption,
) error {
	return c.Client.Create(ctx, obj, opts...) //nolint:wrapcheck // error from client is already contextual
}

func (c *ctrlClientImpl) Delete(
	ctx context.Context, obj client.Object, opts ...client.DeleteOption,
) error {
	return c.Client.Delete(ctx, obj, opts...) //nolint:wrapcheck // error from client is already contextual
}

func (c *ctrlClientImpl) Update(
	ctx context.Context, obj client.Object, opts ...client.UpdateOption,
) error {
	return c.Client.Update(ctx, obj, opts...) //nolint:wrapcheck // error from client is already contextual
}

func (c *ctrlClientImpl) UpdateWithRetry(
	ctx context.Context, obj client.Object, opts ...client.UpdateOption,
) error {
	key := client.ObjectKeyFromObject(obj)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentInterface := reflect.New(reflect.TypeOf(obj).Elem()).Interface()
		current, ok := currentInterface.(client.Object)
		if !ok {
			return fmt.Errorf("failed to create client.Object from type %T", obj)
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
		return err //nolint:wrapcheck // retry error is already contextual
	}

	return nil
}

func (c *ctrlClientImpl) StatusUpdate(
	ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption,
) error {
	return c.Client.Status().Update(ctx, obj, opts...) //nolint:wrapcheck // error from client is already contextual
}

func (c *ctrlClientImpl) Patch(
	ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption,
) error {
	return c.Client.Patch(ctx, obj, patch, opts...) //nolint:wrapcheck // error from client is already contextual
}

func (c *ctrlClientImpl) Exists(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
	if err := c.Client.Get(ctx, key, obj); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err //nolint:wrapcheck // error from client is already contextual
	}
	return true, nil
}
