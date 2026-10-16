package istiocsr

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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
	if err := certmanagerv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

// updateStatus is for updating the status subresource of istiocsr.openshift.operator.io.
func (r *Reconciler) updateStatus(ctx context.Context, changed *v1alpha1.IstioCSR) error {
	namespacedName := client.ObjectKeyFromObject(changed)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		r.log.V(4).Info("updating istiocsr.openshift.operator.io status", "request", namespacedName)
		current := &v1alpha1.IstioCSR{}
		if err := r.Get(ctx, namespacedName, current); err != nil {
			return fmt.Errorf("failed to fetch istiocsr.openshift.operator.io %q for status update: %w", namespacedName, err)
		}
		changed.Status.DeepCopyInto(&current.Status)

		if err := r.StatusUpdate(ctx, current); err != nil {
			return fmt.Errorf("failed to update istiocsr.openshift.operator.io %q status: %w", namespacedName, err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to update status for %q: %w", namespacedName, err)
	}

	return nil
}

// addFinalizer adds finalizer to istiocsr.openshift.operator.io resource.
func (r *Reconciler) addFinalizer(ctx context.Context, istiocsr *v1alpha1.IstioCSR) error {
	namespacedName := client.ObjectKeyFromObject(istiocsr)
	if !controllerutil.ContainsFinalizer(istiocsr, finalizer) {
		if !controllerutil.AddFinalizer(istiocsr, finalizer) {
			return fmt.Errorf("failed to create %q istiocsr.openshift.operator.io object with finalizers added", namespacedName)
		}

		// update istiocsr.openshift.operator.io on adding finalizer.
		if err := r.UpdateWithRetry(ctx, istiocsr); err != nil {
			return fmt.Errorf("failed to add finalizers on %q istiocsr.openshift.operator.io with %w", namespacedName, err)
		}

		updated := &v1alpha1.IstioCSR{}
		if err := r.Get(ctx, namespacedName, updated); err != nil {
			return fmt.Errorf("failed to fetch istiocsr.openshift.operator.io %q after updating finalizers: %w", namespacedName, err)
		}
		updated.DeepCopyInto(istiocsr)
		return nil
	}
	return nil
}

// removeFinalizer removes finalizers added to istiocsr.openshift.operator.io resource.
func (r *Reconciler) removeFinalizer(ctx context.Context, istiocsr *v1alpha1.IstioCSR, finalizer string) error {
	namespacedName := client.ObjectKeyFromObject(istiocsr)
	if controllerutil.ContainsFinalizer(istiocsr, finalizer) {
		if !controllerutil.RemoveFinalizer(istiocsr, finalizer) {
			return fmt.Errorf("failed to create %q istiocsr.openshift.operator.io object with finalizers removed", namespacedName)
		}

		if err := r.UpdateWithRetry(ctx, istiocsr); err != nil {
			return fmt.Errorf("failed to remove finalizers on %q istiocsr.openshift.operator.io with %w", namespacedName, err)
		}
		return nil
	}

	return nil
}

func containsProcessedAnnotation(istiocsr *v1alpha1.IstioCSR) bool {
	return common.ContainsAnnotation(istiocsr, controllerProcessedAnnotation)
}

func containsProcessingRejectedAnnotation(istiocsr *v1alpha1.IstioCSR) bool {
	return common.ContainsAnnotation(istiocsr, controllerProcessingRejectedAnnotation)
}

func addProcessedAnnotation(istiocsr *v1alpha1.IstioCSR) bool {
	return common.AddAnnotation(istiocsr, controllerProcessedAnnotation, "true")
}

func addProcessingRejectedAnnotation(istiocsr *v1alpha1.IstioCSR) bool {
	return common.AddAnnotation(istiocsr, controllerProcessingRejectedAnnotation, "true")
}

func updateResourceLabelsWithIstioMapperLabels(obj client.Object, istiocsrNamespace string, labels map[string]string) {
	l := make(map[string]string, len(labels)+1)
	maps.Copy(l, labels)
	l[istiocsrNamespaceMappingLabelName] = istiocsrNamespace
	obj.SetLabels(l)
}

func decodeDeploymentObjBytes(objBytes []byte) *appsv1.Deployment {
	obj, err := runtime.Decode(codecs.UniversalDecoder(appsv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		panic("failed to convert to *appsv1.Deployment")
	}
	return deployment
}

func decodeClusterRoleObjBytes(objBytes []byte) *rbacv1.ClusterRole {
	obj, err := runtime.Decode(codecs.UniversalDecoder(rbacv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	clusterRole, ok := obj.(*rbacv1.ClusterRole)
	if !ok {
		panic("failed to convert to *rbacv1.ClusterRole")
	}
	return clusterRole
}

func decodeClusterRoleBindingObjBytes(objBytes []byte) *rbacv1.ClusterRoleBinding {
	obj, err := runtime.Decode(codecs.UniversalDecoder(rbacv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	clusterRoleBinding, ok := obj.(*rbacv1.ClusterRoleBinding)
	if !ok {
		panic("failed to convert to *rbacv1.ClusterRoleBinding")
	}
	return clusterRoleBinding
}

func decodeRoleObjBytes(objBytes []byte) *rbacv1.Role {
	obj, err := runtime.Decode(codecs.UniversalDecoder(rbacv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	role, ok := obj.(*rbacv1.Role)
	if !ok {
		panic("failed to convert to *rbacv1.Role")
	}
	return role
}

func decodeRoleBindingObjBytes(objBytes []byte) *rbacv1.RoleBinding {
	obj, err := runtime.Decode(codecs.UniversalDecoder(rbacv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	roleBinding, ok := obj.(*rbacv1.RoleBinding)
	if !ok {
		panic("failed to convert to *rbacv1.RoleBinding")
	}
	return roleBinding
}

func decodeServiceObjBytes(objBytes []byte) *corev1.Service {
	obj, err := runtime.Decode(codecs.UniversalDecoder(corev1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	service, ok := obj.(*corev1.Service)
	if !ok {
		panic("failed to convert to *corev1.Service")
	}
	return service
}

func decodeServiceAccountObjBytes(objBytes []byte) *corev1.ServiceAccount {
	obj, err := runtime.Decode(codecs.UniversalDecoder(corev1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	serviceAccount, ok := obj.(*corev1.ServiceAccount)
	if !ok {
		panic("failed to convert to *corev1.ServiceAccount")
	}
	return serviceAccount
}

func decodeCertificateObjBytes(objBytes []byte) *certmanagerv1.Certificate {
	obj, err := runtime.Decode(codecs.UniversalDecoder(certmanagerv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	certificate, ok := obj.(*certmanagerv1.Certificate)
	if !ok {
		panic("failed to convert to *certmanagerv1.Certificate")
	}
	return certificate
}

func validateIstioCSRConfig(istiocsr *v1alpha1.IstioCSR) error {
	if reflect.ValueOf(istiocsr.Spec.IstioCSRConfig).IsZero() {
		return fmt.Errorf("spec.istioCSRConfig config cannot be empty")
	}
	if reflect.ValueOf(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig).IsZero() {
		return fmt.Errorf("spec.istioCSRConfig.istiodTLSConfig config cannot be empty")
	}
	if reflect.ValueOf(istiocsr.Spec.IstioCSRConfig.Istio).IsZero() {
		return fmt.Errorf("spec.istioCSRConfig.istio config cannot be empty")
	}
	if reflect.ValueOf(istiocsr.Spec.IstioCSRConfig.CertManager).IsZero() {
		return fmt.Errorf("spec.istioCSRConfig.certManager config cannot be empty")
	}
	return nil
}

func (r *Reconciler) updateCondition(istiocsr *v1alpha1.IstioCSR, prependErr error) error {
	if err := r.updateStatus(r.ctx, istiocsr); err != nil {
		errUpdate := fmt.Errorf("failed to update %s/%s status: %w", istiocsr.GetNamespace(), istiocsr.GetName(), err)
		if prependErr != nil {
			return utilerrors.NewAggregate([]error{err, errUpdate})
		}
		return errUpdate
	}
	return prependErr
}

func (r *Reconciler) disallowMultipleIstioCSRInstances(istiocsr *v1alpha1.IstioCSR) error {
	statusMessage := fmt.Sprintf("multiple instances of istiocsr exists, %s/%s will not be processed", istiocsr.GetNamespace(), istiocsr.GetName())

	if containsProcessingRejectedAnnotation(istiocsr) {
		r.log.V(4).Info("%s/%s istiocsr resource contains processing rejected annotation", istiocsr.Namespace, istiocsr.Name)
		// ensure status is updated.
		var updateErr error
		if istiocsr.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonFailed, statusMessage) {
			updateErr = r.updateCondition(istiocsr, nil)
		}
		return common.NewMultipleInstanceError(utilerrors.NewAggregate([]error{errors.New(statusMessage), updateErr}))
	}

	istiocsrList := &v1alpha1.IstioCSRList{}
	if err := r.List(r.ctx, istiocsrList); err != nil {
		return fmt.Errorf("failed to fetch list of istiocsr resources: %w", err)
	}

	if len(istiocsrList.Items) <= 1 {
		return nil
	}

	ignoreProcessing := false
	for _, item := range istiocsrList.Items {
		if item.GetNamespace() == istiocsr.Namespace {
			continue
		}
		if item.CreationTimestamp.Time.Before(istiocsr.CreationTimestamp.Time) ||
			// Even when timestamps are equal will skip processing. And if this ends
			// up in ignoring all istiocsr instances, which means user must have created
			// all in parallel, onus is on user to delete all and recreate just one required
			// instance of istiocsr.
			item.CreationTimestamp.Time.Equal(istiocsr.CreationTimestamp.Time) {
			ignoreProcessing = true
		}
	}

	if !ignoreProcessing {
		// This is the oldest instance, allow it to proceed
		return nil
	}

	// This instance should be rejected as there's an older or equally old instance
	var condUpdateErr, annUpdateErr error
	if istiocsr.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonFailed, statusMessage) {
		condUpdateErr = r.updateCondition(istiocsr, nil)
	}
	if addProcessingRejectedAnnotation(istiocsr) {
		if err := r.UpdateWithRetry(r.ctx, istiocsr); err != nil {
			annUpdateErr = fmt.Errorf("failed to update reject processing annotation to %s/%s: %w", istiocsr.GetNamespace(), istiocsr.GetName(), err)
		}
	}
	if condUpdateErr != nil || annUpdateErr != nil {
		return utilerrors.NewAggregate([]error{condUpdateErr, annUpdateErr})
	}

	return common.NewMultipleInstanceError(errors.New(statusMessage))
}
