package trustmanager

import (
	"context"
	"fmt"
	"reflect"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
)

var (
	localScheme = runtime.NewScheme()
	codecs      = serializer.NewCodecFactory(localScheme)
)

func init() {
	if err := appsv1.AddToScheme(localScheme); err != nil {
		panic(err)
	}
	if err := corev1.AddToScheme(localScheme); err != nil {
		panic(err)
	}
	if err := rbacv1.AddToScheme(localScheme); err != nil {
		panic(err)
	}
	if err := certmanagerv1.AddToScheme(localScheme); err != nil {
		panic(err)
	}
	if err := admissionregistrationv1.AddToScheme(localScheme); err != nil {
		panic(err)
	}
}

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
func (r *Reconciler) removeFinalizer(ctx context.Context, trustManager *v1alpha1.TrustManager, fin string) error {
	namespacedName := client.ObjectKeyFromObject(trustManager)
	if controllerutil.ContainsFinalizer(trustManager, fin) {
		if !controllerutil.RemoveFinalizer(trustManager, fin) {
			return fmt.Errorf("failed to create %q trustmanagers.operator.openshift.io object with finalizers removed", namespacedName)
		}

		if err := r.UpdateWithRetry(ctx, trustManager); err != nil {
			return fmt.Errorf("failed to remove finalizers on %q trustmanagers.operator.openshift.io with %w", namespacedName, err)
		}
		return nil
	}

	return nil
}

func containsProcessedAnnotation(trustManager *v1alpha1.TrustManager) bool {
	_, exist := trustManager.GetAnnotations()[controllerProcessedAnnotation]
	return exist
}

func containsProcessingRejectedAnnotation(trustManager *v1alpha1.TrustManager) bool {
	_, exist := trustManager.GetAnnotations()[controllerProcessingRejectedAnnotation]
	return exist
}

func addProcessedAnnotation(trustManager *v1alpha1.TrustManager) bool {
	annotations := trustManager.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	if _, exist := annotations[controllerProcessedAnnotation]; !exist {
		annotations[controllerProcessedAnnotation] = "true"
		trustManager.SetAnnotations(annotations)
		return true
	}
	return false
}

func addProcessingRejectedAnnotation(trustManager *v1alpha1.TrustManager) bool {
	annotations := trustManager.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	if _, exist := annotations[controllerProcessingRejectedAnnotation]; !exist {
		annotations[controllerProcessingRejectedAnnotation] = "true"
		trustManager.SetAnnotations(annotations)
		return true
	}
	return false
}

func updateNamespace(obj client.Object, newNamespace string) {
	obj.SetNamespace(newNamespace)
}

func updateResourceLabels(obj client.Object, labels map[string]string) {
	obj.SetLabels(labels)
}

func decodeDeploymentObjBytes(objBytes []byte) *appsv1.Deployment {
	obj, err := runtime.Decode(codecs.UniversalDecoder(appsv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*appsv1.Deployment)
}

func decodeClusterRoleObjBytes(objBytes []byte) *rbacv1.ClusterRole {
	obj, err := runtime.Decode(codecs.UniversalDecoder(rbacv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*rbacv1.ClusterRole)
}

func decodeClusterRoleBindingObjBytes(objBytes []byte) *rbacv1.ClusterRoleBinding {
	obj, err := runtime.Decode(codecs.UniversalDecoder(rbacv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*rbacv1.ClusterRoleBinding)
}

func decodeRoleObjBytes(objBytes []byte) *rbacv1.Role {
	obj, err := runtime.Decode(codecs.UniversalDecoder(rbacv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*rbacv1.Role)
}

func decodeRoleBindingObjBytes(objBytes []byte) *rbacv1.RoleBinding {
	obj, err := runtime.Decode(codecs.UniversalDecoder(rbacv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*rbacv1.RoleBinding)
}

func decodeServiceObjBytes(objBytes []byte) *corev1.Service {
	obj, err := runtime.Decode(codecs.UniversalDecoder(corev1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*corev1.Service)
}

func decodeServiceAccountObjBytes(objBytes []byte) *corev1.ServiceAccount {
	obj, err := runtime.Decode(codecs.UniversalDecoder(corev1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*corev1.ServiceAccount)
}

func decodeCertificateObjBytes(objBytes []byte) *certmanagerv1.Certificate {
	obj, err := runtime.Decode(codecs.UniversalDecoder(certmanagerv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*certmanagerv1.Certificate)
}

func decodeIssuerObjBytes(objBytes []byte) *certmanagerv1.Issuer {
	obj, err := runtime.Decode(codecs.UniversalDecoder(certmanagerv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*certmanagerv1.Issuer)
}

func decodeValidatingWebhookConfigurationObjBytes(objBytes []byte) *admissionregistrationv1.ValidatingWebhookConfiguration {
	obj, err := runtime.Decode(codecs.UniversalDecoder(admissionregistrationv1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return obj.(*admissionregistrationv1.ValidatingWebhookConfiguration)
}

func hasObjectChanged(desired, fetched client.Object) bool {
	if reflect.TypeOf(desired) != reflect.TypeOf(fetched) {
		panic("both objects to be compared must be of same type")
	}

	var objectModified bool
	switch desired.(type) {
	case *certmanagerv1.Certificate:
		objectModified = !reflect.DeepEqual(desired.(*certmanagerv1.Certificate).Spec, fetched.(*certmanagerv1.Certificate).Spec)
	case *certmanagerv1.Issuer:
		objectModified = !reflect.DeepEqual(desired.(*certmanagerv1.Issuer).Spec, fetched.(*certmanagerv1.Issuer).Spec)
	case *rbacv1.ClusterRole:
		objectModified = !reflect.DeepEqual(desired.(*rbacv1.ClusterRole).Rules, fetched.(*rbacv1.ClusterRole).Rules)
	case *rbacv1.ClusterRoleBinding:
		objectModified = !reflect.DeepEqual(desired.(*rbacv1.ClusterRoleBinding).RoleRef, fetched.(*rbacv1.ClusterRoleBinding).RoleRef) ||
			!reflect.DeepEqual(desired.(*rbacv1.ClusterRoleBinding).Subjects, fetched.(*rbacv1.ClusterRoleBinding).Subjects)
	case *appsv1.Deployment:
		objectModified = deploymentSpecModified(desired.(*appsv1.Deployment), fetched.(*appsv1.Deployment))
	case *rbacv1.Role:
		objectModified = !reflect.DeepEqual(desired.(*rbacv1.Role).Rules, fetched.(*rbacv1.Role).Rules)
	case *rbacv1.RoleBinding:
		objectModified = !reflect.DeepEqual(desired.(*rbacv1.RoleBinding).RoleRef, fetched.(*rbacv1.RoleBinding).RoleRef) ||
			!reflect.DeepEqual(desired.(*rbacv1.RoleBinding).Subjects, fetched.(*rbacv1.RoleBinding).Subjects)
	case *corev1.Service:
		objectModified = serviceSpecModified(desired.(*corev1.Service), fetched.(*corev1.Service))
	case *corev1.ConfigMap:
		objectModified = !reflect.DeepEqual(desired.(*corev1.ConfigMap).Data, fetched.(*corev1.ConfigMap).Data)
	case *admissionregistrationv1.ValidatingWebhookConfiguration:
		objectModified = !reflect.DeepEqual(desired.(*admissionregistrationv1.ValidatingWebhookConfiguration).Webhooks, fetched.(*admissionregistrationv1.ValidatingWebhookConfiguration).Webhooks)
	default:
		panic(fmt.Sprintf("unsupported object type: %T", desired))
	}
	return objectModified || !reflect.DeepEqual(desired.GetLabels(), fetched.GetLabels())
}

func deploymentSpecModified(desired, fetched *appsv1.Deployment) bool {
	if *desired.Spec.Replicas != *fetched.Spec.Replicas ||
		!reflect.DeepEqual(desired.Spec.Selector.MatchLabels, fetched.Spec.Selector.MatchLabels) {
		return true
	}

	if !reflect.DeepEqual(desired.Spec.Template.Labels, fetched.Spec.Template.Labels) ||
		len(desired.Spec.Template.Spec.Containers) != len(fetched.Spec.Template.Spec.Containers) {
		return true
	}

	desiredContainer := desired.Spec.Template.Spec.Containers[0]
	fetchedContainer := fetched.Spec.Template.Spec.Containers[0]
	if !reflect.DeepEqual(desiredContainer.Args, fetchedContainer.Args) ||
		desiredContainer.Name != fetchedContainer.Name || desiredContainer.Image != fetchedContainer.Image ||
		desiredContainer.ImagePullPolicy != fetchedContainer.ImagePullPolicy {
		return true
	}

	if len(desiredContainer.Ports) != len(fetchedContainer.Ports) {
		return true
	}
	for _, fetchedPort := range fetchedContainer.Ports {
		matched := false
		for _, desiredPort := range desiredContainer.Ports {
			if fetchedPort.ContainerPort == desiredPort.ContainerPort {
				matched = true
				break
			}
		}
		if !matched {
			return true
		}
	}

	if desiredContainer.ReadinessProbe.HTTPGet.Path != fetchedContainer.ReadinessProbe.HTTPGet.Path ||
		desiredContainer.ReadinessProbe.InitialDelaySeconds != fetchedContainer.ReadinessProbe.InitialDelaySeconds ||
		desiredContainer.ReadinessProbe.PeriodSeconds != fetchedContainer.ReadinessProbe.PeriodSeconds {
		return true
	}

	if !reflect.DeepEqual(desiredContainer.Resources, fetchedContainer.Resources) ||
		!reflect.DeepEqual(*desiredContainer.SecurityContext, *fetchedContainer.SecurityContext) ||
		!reflect.DeepEqual(desiredContainer.VolumeMounts, fetchedContainer.VolumeMounts) {
		return true
	}

	if desired.Spec.Template.Spec.ServiceAccountName != fetched.Spec.Template.Spec.ServiceAccountName ||
		!reflect.DeepEqual(desired.Spec.Template.Spec.NodeSelector, fetched.Spec.Template.Spec.NodeSelector) ||
		!reflect.DeepEqual(desired.Spec.Template.Spec.Volumes, fetched.Spec.Template.Spec.Volumes) {
		return true
	}

	return false
}

func serviceSpecModified(desired, fetched *corev1.Service) bool {
	if desired.Spec.Type != fetched.Spec.Type ||
		!reflect.DeepEqual(desired.Spec.Ports, fetched.Spec.Ports) ||
		!reflect.DeepEqual(desired.Spec.Selector, fetched.Spec.Selector) {
		return true
	}

	return false
}

func validateTrustManagerConfig(trustManager *v1alpha1.TrustManager) error {
	if reflect.ValueOf(trustManager.Spec.TrustManagerConfig).IsZero() {
		return fmt.Errorf("spec.trustManagerConfig config cannot be empty")
	}
	return nil
}

func (r *Reconciler) updateCondition(trustManager *v1alpha1.TrustManager, prependErr error) error {
	if err := r.updateStatus(r.ctx, trustManager); err != nil {
		errUpdate := fmt.Errorf("failed to update %s status: %w", trustManager.GetName(), err)
		if prependErr != nil {
			return utilerrors.NewAggregate([]error{err, errUpdate})
		}
		return errUpdate
	}
	return prependErr
}

func (r *Reconciler) disallowMultipleTrustManagerInstances(trustManager *v1alpha1.TrustManager) error {
	statusMessage := fmt.Sprintf("multiple instances of trustmanager exists, %s will not be processed", trustManager.GetName())

	if containsProcessingRejectedAnnotation(trustManager) {
		r.log.V(4).Info("trustmanager resource contains processing rejected annotation", "name", trustManager.Name)
		var updateErr error
		if trustManager.Status.ConditionalStatus.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonFailed, statusMessage) {
			updateErr = r.updateCondition(trustManager, nil)
		}
		return newMultipleInstanceError(utilerrors.NewAggregate([]error{fmt.Errorf("%s", statusMessage), updateErr}))
	}

	trustManagerList := &v1alpha1.TrustManagerList{}
	if err := r.List(r.ctx, trustManagerList); err != nil {
		return fmt.Errorf("failed to fetch list of trustmanager resources: %w", err)
	}

	if len(trustManagerList.Items) <= 1 {
		return nil
	}

	ignoreProcessing := false
	for _, item := range trustManagerList.Items {
		if item.GetName() == trustManager.GetName() {
			continue
		}
		if item.CreationTimestamp.Time.Before(trustManager.CreationTimestamp.Time) ||
			item.CreationTimestamp.Time.Equal(trustManager.CreationTimestamp.Time) {
			ignoreProcessing = true
		}
	}

	if ignoreProcessing {
		var condUpdateErr, annUpdateErr error
		if trustManager.Status.ConditionalStatus.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonFailed, statusMessage) {
			condUpdateErr = r.updateCondition(trustManager, nil)
		}
		if addProcessingRejectedAnnotation(trustManager) {
			if err := r.UpdateWithRetry(r.ctx, trustManager); err != nil {
				annUpdateErr = fmt.Errorf("failed to update reject processing annotation to %s: %w", trustManager.GetName(), err)
			}
		}
		if condUpdateErr != nil || annUpdateErr != nil {
			return utilerrors.NewAggregate([]error{condUpdateErr, annUpdateErr})
		}
	}

	return newMultipleInstanceError(fmt.Errorf("%s", statusMessage))
}
