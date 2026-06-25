package http01proxy

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

func init() {
	if err := appsv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := networkingv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := rbacv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

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

func hasObjectChanged(desired, fetched client.Object) (bool, error) {
	if reflect.TypeOf(desired) != reflect.TypeOf(fetched) {
		return false, fmt.Errorf("type mismatch: desired %T vs fetched %T", desired, fetched)
	}

	var objectModified bool
	switch d := desired.(type) {
	case *rbacv1.ClusterRole:
		f, _ := fetched.(*rbacv1.ClusterRole)
		objectModified = !reflect.DeepEqual(d.Rules, f.Rules)
	case *rbacv1.ClusterRoleBinding:
		f, _ := fetched.(*rbacv1.ClusterRoleBinding)
		objectModified = !reflect.DeepEqual(d.RoleRef, f.RoleRef) || !reflect.DeepEqual(d.Subjects, f.Subjects)
	case *appsv1.DaemonSet:
		f, _ := fetched.(*appsv1.DaemonSet)
		objectModified = daemonSetSpecModified(d, f)
	case *networkingv1.NetworkPolicy:
		f, _ := fetched.(*networkingv1.NetworkPolicy)
		objectModified = !reflect.DeepEqual(d.Spec, f.Spec)
	case *corev1.ServiceAccount:
		return common.ObjectMetadataModified(desired, fetched), nil
	default:
		return false, fmt.Errorf("unsupported object type: %T", desired)
	}
	return objectModified || common.ObjectMetadataModified(desired, fetched), nil
}

func daemonSetSpecModified(desired, fetched *appsv1.DaemonSet) bool {
	if !reflect.DeepEqual(desired.Spec.Selector.MatchLabels, fetched.Spec.Selector.MatchLabels) {
		return true
	}
	if !reflect.DeepEqual(desired.Spec.Template.Labels, fetched.Spec.Template.Labels) ||
		len(desired.Spec.Template.Spec.Containers) != len(fetched.Spec.Template.Spec.Containers) {
		return true
	}
	if len(desired.Spec.Template.Spec.Containers) > 0 && len(fetched.Spec.Template.Spec.Containers) > 0 {
		desiredContainer := desired.Spec.Template.Spec.Containers[0]
		fetchedContainer := fetched.Spec.Template.Spec.Containers[0]
		if desiredContainer.Image != fetchedContainer.Image ||
			desiredContainer.Name != fetchedContainer.Name ||
			!reflect.DeepEqual(desiredContainer.Env, fetchedContainer.Env) ||
			!reflect.DeepEqual(desiredContainer.Ports, fetchedContainer.Ports) {
			return true
		}
	}
	if desired.Spec.Template.Spec.ServiceAccountName != fetched.Spec.Template.Spec.ServiceAccountName ||
		!reflect.DeepEqual(desired.Spec.Template.Spec.NodeSelector, fetched.Spec.Template.Spec.NodeSelector) {
		return true
	}
	return false
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

func (r *Reconciler) getInternalPort(proxy *v1alpha1.HTTP01Proxy) int32 {
	if proxy.Spec.Mode == v1alpha1.HTTP01ProxyModeCustom &&
		proxy.Spec.CustomDeployment != nil &&
		proxy.Spec.CustomDeployment.InternalPort > 0 {
		return proxy.Spec.CustomDeployment.InternalPort
	}
	return defaultInternalPort
}

func (r *Reconciler) createOrUpdateResource(ctx context.Context, desired client.Object) error {
	key := client.ObjectKeyFromObject(desired)
	kind := desired.GetObjectKind().GroupVersionKind().Kind

	r.log.V(2).Info("creating resource", "kind", kind, "name", key)
	if err := r.Create(ctx, desired); err != nil {
		if !errors.IsAlreadyExists(err) {
			return common.FromClientError(err, "failed to create %s %q", kind, key)
		}
		fetched, _ := desired.DeepCopyObject().(client.Object)
		if err := r.Get(ctx, key, fetched); err != nil {
			return common.FromClientError(err, "failed to get %s %q for update", kind, key)
		}
		changed, err := hasObjectChanged(desired, fetched)
		if err != nil {
			return fmt.Errorf("failed to compare %s %q: %w", kind, key, err)
		}
		if changed {
			r.log.V(2).Info("updating resource", "kind", kind, "name", key)
			desired.SetResourceVersion(fetched.GetResourceVersion())
			if err := r.Update(ctx, desired); err != nil {
				return common.FromClientError(err, "failed to update %s %q", kind, key)
			}
		}
	}

	return nil
}

func (r *Reconciler) deleteIfExists(ctx context.Context, obj client.Object, key client.ObjectKey) error {
	obj.SetName(key.Name)
	obj.SetNamespace(key.Namespace)
	if err := r.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete %s %q: %w", obj.GetObjectKind().GroupVersionKind().Kind, key, err)
	}
	return nil
}
