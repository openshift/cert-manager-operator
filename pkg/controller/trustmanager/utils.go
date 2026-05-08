package trustmanager

import (
	"context"
	"fmt"
	"os"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// updateStatus is for updating the status subresource of trustmanagers.operator.openshift.io.
func (r *Reconciler) updateStatus(ctx context.Context, changed *v1alpha1.TrustManager) error {
	namespacedName := client.ObjectKeyFromObject(changed)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		r.log.V(4).Info("updating trustmanagers.operator.openshift.io status", "request", namespacedName)
		current := &v1alpha1.TrustManager{}
		if err := r.Get(ctx, namespacedName, current); err != nil {
			return fmt.Errorf("failed to fetch trustmanagers.operator.openshift.io %q for status update: %w", namespacedName, err)
		}
		changed.Status.DeepCopyInto(&current.Status)

		if err := r.StatusUpdate(ctx, current); err != nil {
			return fmt.Errorf("failed to update trustmanagers.operator.openshift.io %q status: %w", namespacedName, err)
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

// addFinalizer adds finalizer to trustmanagers.operator.openshift.io resource.
func (r *Reconciler) addFinalizer(ctx context.Context, trustManager *v1alpha1.TrustManager) error {
	namespacedName := client.ObjectKeyFromObject(trustManager)
	if !controllerutil.ContainsFinalizer(trustManager, finalizer) {
		if !controllerutil.AddFinalizer(trustManager, finalizer) {
			return fmt.Errorf("failed to create %q trustmanagers.operator.openshift.io object with finalizers added", namespacedName)
		}

		// update trustmanagers.operator.openshift.io on adding finalizer.
		if err := r.UpdateWithRetry(ctx, trustManager); err != nil {
			return fmt.Errorf("failed to add finalizers on %q trustmanagers.operator.openshift.io with %w", namespacedName, err)
		}

		updated := &v1alpha1.TrustManager{}
		if err := r.Get(ctx, namespacedName, updated); err != nil {
			return fmt.Errorf("failed to fetch trustmanagers.operator.openshift.io %q after updating finalizers: %w", namespacedName, err)
		}
		updated.DeepCopyInto(trustManager)
		return nil
	}
	return nil
}

// removeFinalizer removes finalizers added to trustmanagers.operator.openshift.io resource.
func (r *Reconciler) removeFinalizer(ctx context.Context, trustManager *v1alpha1.TrustManager, finalizer string) error {
	namespacedName := client.ObjectKeyFromObject(trustManager)
	if controllerutil.ContainsFinalizer(trustManager, finalizer) {
		if !controllerutil.RemoveFinalizer(trustManager, finalizer) {
			return fmt.Errorf("failed to create %q trustmanagers.operator.openshift.io object with finalizers removed", namespacedName)
		}

		if err := r.UpdateWithRetry(ctx, trustManager); err != nil {
			return fmt.Errorf("failed to remove finalizers on %q trustmanagers.operator.openshift.io with %w", namespacedName, err)
		}
		return nil
	}

	return nil
}

// updateCondition updates the status of the trustmanagers.operator.openshift.io resource.
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

// updateObservedStatus updates the observed state fields in the TrustManager status.
func (r *Reconciler) updateObservedStatus(trustManager *v1alpha1.TrustManager) {
	trustManager.Status.TrustManagerImage = os.Getenv(trustManagerImageNameEnvVarName)
	trustManager.Status.TrustNamespace = trustManager.Spec.TrustManagerConfig.TrustNamespace
	if trustManager.Status.TrustNamespace == "" {
		trustManager.Status.TrustNamespace = operandNamespace
	}
	trustManager.Status.SecretTargetsPolicy = trustManager.Spec.TrustManagerConfig.SecretTargets.Policy
	trustManager.Status.DefaultCAPackagePolicy = trustManager.Spec.TrustManagerConfig.DefaultCAPackage.Policy
	trustManager.Status.FilterExpiredCertificatesPolicy = trustManager.Spec.TrustManagerConfig.FilterExpiredCertificates
}

func objectMetadataModified(desired, fetched client.Object) bool {
	return !reflect.DeepEqual(desired.GetLabels(), fetched.GetLabels())
}

func configMapDataModified(desired, fetched *corev1.ConfigMap) bool {
	return !reflect.DeepEqual(desired.Data, fetched.Data)
}

func (r *Reconciler) createOrUpdateClusterRole(desired *rbacv1.ClusterRole, trustManager *v1alpha1.TrustManager) error {
	existing := &rbacv1.ClusterRole{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return fmt.Errorf("failed to check ClusterRole %q existence: %w", desired.Name, err)
	}

	if !exists {
		r.log.Info("creating ClusterRole", "name", desired.Name)
		if err := r.Create(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to create ClusterRole %q: %w", desired.Name, err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created ClusterRole %s", desired.Name)
		return nil
	}

	if !reflect.DeepEqual(existing.Rules, desired.Rules) || objectMetadataModified(desired, existing) {
		existing.Rules = desired.Rules
		existing.Labels = desired.Labels
		r.log.Info("updating ClusterRole", "name", desired.Name)
		if err := r.Update(r.ctx, existing); err != nil {
			return fmt.Errorf("failed to update ClusterRole %q: %w", desired.Name, err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Updated", "Updated ClusterRole %s", desired.Name)
	}

	return nil
}

func (r *Reconciler) createOrUpdateClusterRoleBinding(desired *rbacv1.ClusterRoleBinding, trustManager *v1alpha1.TrustManager) error {
	existing := &rbacv1.ClusterRoleBinding{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return fmt.Errorf("failed to check ClusterRoleBinding %q existence: %w", desired.Name, err)
	}

	if !exists {
		r.log.Info("creating ClusterRoleBinding", "name", desired.Name)
		if err := r.Create(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to create ClusterRoleBinding %q: %w", desired.Name, err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created ClusterRoleBinding %s", desired.Name)
		return nil
	}

	if !reflect.DeepEqual(existing.RoleRef, desired.RoleRef) ||
		!reflect.DeepEqual(existing.Subjects, desired.Subjects) ||
		objectMetadataModified(desired, existing) {
		existing.RoleRef = desired.RoleRef
		existing.Subjects = desired.Subjects
		existing.Labels = desired.Labels
		r.log.Info("updating ClusterRoleBinding", "name", desired.Name)
		if err := r.Update(r.ctx, existing); err != nil {
			return fmt.Errorf("failed to update ClusterRoleBinding %q: %w", desired.Name, err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Updated", "Updated ClusterRoleBinding %s", desired.Name)
	}

	return nil
}

func (r *Reconciler) createOrUpdateRole(desired *rbacv1.Role, trustManager *v1alpha1.TrustManager) error {
	existing := &rbacv1.Role{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return fmt.Errorf("failed to check Role %s/%s existence: %w", desired.Namespace, desired.Name, err)
	}

	if !exists {
		r.log.Info("creating Role", "name", desired.Name, "namespace", desired.Namespace)
		if err := r.Create(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to create Role %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created Role %s/%s", desired.Namespace, desired.Name)
		return nil
	}

	if !reflect.DeepEqual(existing.Rules, desired.Rules) || objectMetadataModified(desired, existing) {
		existing.Rules = desired.Rules
		existing.Labels = desired.Labels
		r.log.Info("updating Role", "name", desired.Name, "namespace", desired.Namespace)
		if err := r.Update(r.ctx, existing); err != nil {
			return fmt.Errorf("failed to update Role %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Updated", "Updated Role %s/%s", desired.Namespace, desired.Name)
	}

	return nil
}

func (r *Reconciler) createOrUpdateRoleBinding(desired *rbacv1.RoleBinding, trustManager *v1alpha1.TrustManager) error {
	existing := &rbacv1.RoleBinding{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return fmt.Errorf("failed to check RoleBinding %s/%s existence: %w", desired.Namespace, desired.Name, err)
	}

	if !exists {
		r.log.Info("creating RoleBinding", "name", desired.Name, "namespace", desired.Namespace)
		if err := r.Create(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to create RoleBinding %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created RoleBinding %s/%s", desired.Namespace, desired.Name)
		return nil
	}

	if !reflect.DeepEqual(existing.RoleRef, desired.RoleRef) ||
		!reflect.DeepEqual(existing.Subjects, desired.Subjects) ||
		objectMetadataModified(desired, existing) {
		existing.RoleRef = desired.RoleRef
		existing.Subjects = desired.Subjects
		existing.Labels = desired.Labels
		r.log.Info("updating RoleBinding", "name", desired.Name, "namespace", desired.Namespace)
		if err := r.Update(r.ctx, existing); err != nil {
			return fmt.Errorf("failed to update RoleBinding %s/%s: %w", desired.Namespace, desired.Name, err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Updated", "Updated RoleBinding %s/%s", desired.Namespace, desired.Name)
	}

	return nil
}
