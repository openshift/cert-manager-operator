package istiocsr

import (
	"context"
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
		return err
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
	_, exist := istiocsr.GetAnnotations()[controllerProcessedAnnotation]
	return exist
}

func containsProcessingRejectedAnnotation(istiocsr *v1alpha1.IstioCSR) bool {
	_, exist := istiocsr.GetAnnotations()[controllerProcessingRejectedAnnotation]
	return exist
}

func addProcessedAnnotation(istiocsr *v1alpha1.IstioCSR) bool {
	annotations := istiocsr.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	if _, exist := annotations[controllerProcessedAnnotation]; !exist {
		annotations[controllerProcessedAnnotation] = "true"
		istiocsr.SetAnnotations(annotations)
		return true
	}
	return false
}

func addProcessingRejectedAnnotation(istiocsr *v1alpha1.IstioCSR) bool {
	annotations := istiocsr.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	if _, exist := annotations[controllerProcessingRejectedAnnotation]; !exist {
		annotations[controllerProcessingRejectedAnnotation] = "true"
		istiocsr.SetAnnotations(annotations)
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

func hasObjectChanged(desired, fetched client.Object) bool {
	if reflect.TypeOf(desired) != reflect.TypeOf(fetched) {
		panic("both objects to be compared must be of same type")
	}

	var objectModified bool
	switch desired.(type) {
	case *certmanagerv1.Certificate:
		desiredCert, ok := desired.(*certmanagerv1.Certificate)
		if !ok {
			panic("failed to convert desired to *certmanagerv1.Certificate")
		}
		fetchedCert, ok := fetched.(*certmanagerv1.Certificate)
		if !ok {
			panic("failed to convert fetched to *certmanagerv1.Certificate")
		}
		objectModified = certificateSpecModified(desiredCert, fetchedCert)
	case *rbacv1.ClusterRole:
		desiredClusterRole, ok := desired.(*rbacv1.ClusterRole)
		if !ok {
			panic("failed to convert desired to *rbacv1.ClusterRole")
		}
		fetchedClusterRole, ok := fetched.(*rbacv1.ClusterRole)
		if !ok {
			panic("failed to convert fetched to *rbacv1.ClusterRole")
		}
		objectModified = rbacRoleRulesModified[*rbacv1.ClusterRole](desiredClusterRole, fetchedClusterRole)
	case *rbacv1.ClusterRoleBinding:
		desiredClusterRoleBinding, ok := desired.(*rbacv1.ClusterRoleBinding)
		if !ok {
			panic("failed to convert desired to *rbacv1.ClusterRoleBinding")
		}
		fetchedClusterRoleBinding, ok := fetched.(*rbacv1.ClusterRoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.ClusterRoleBinding")
		}
		objectModified = rbacRoleBindingRefModified[*rbacv1.ClusterRoleBinding](desiredClusterRoleBinding, fetchedClusterRoleBinding) ||
			rbacRoleBindingSubjectsModified[*rbacv1.ClusterRoleBinding](desiredClusterRoleBinding, fetchedClusterRoleBinding)
	case *appsv1.Deployment:
		desiredDeployment, ok := desired.(*appsv1.Deployment)
		if !ok {
			panic("failed to convert desired to *appsv1.Deployment")
		}
		fetchedDeployment, ok := fetched.(*appsv1.Deployment)
		if !ok {
			panic("failed to convert fetched to *appsv1.Deployment")
		}
		objectModified = deploymentSpecModified(desiredDeployment, fetchedDeployment)
	case *rbacv1.Role:
		desiredRole, ok := desired.(*rbacv1.Role)
		if !ok {
			panic("failed to convert desired to *rbacv1.Role")
		}
		fetchedRole, ok := fetched.(*rbacv1.Role)
		if !ok {
			panic("failed to convert fetched to *rbacv1.Role")
		}
		objectModified = rbacRoleRulesModified[*rbacv1.Role](desiredRole, fetchedRole)
	case *rbacv1.RoleBinding:
		desiredRoleBinding, ok := desired.(*rbacv1.RoleBinding)
		if !ok {
			panic("failed to convert desired to *rbacv1.RoleBinding")
		}
		fetchedRoleBinding, ok := fetched.(*rbacv1.RoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.RoleBinding")
		}
		objectModified = rbacRoleBindingRefModified[*rbacv1.RoleBinding](desiredRoleBinding, fetchedRoleBinding) ||
			rbacRoleBindingSubjectsModified[*rbacv1.RoleBinding](desiredRoleBinding, fetchedRoleBinding)
	case *corev1.Service:
		desiredService, ok := desired.(*corev1.Service)
		if !ok {
			panic("failed to convert desired to *corev1.Service")
		}
		fetchedService, ok := fetched.(*corev1.Service)
		if !ok {
			panic("failed to convert fetched to *corev1.Service")
		}
		objectModified = serviceSpecModified(desiredService, fetchedService)
	case *corev1.ConfigMap:
		desiredConfigMap, ok := desired.(*corev1.ConfigMap)
		if !ok {
			panic("failed to convert desired to *corev1.ConfigMap")
		}
		fetchedConfigMap, ok := fetched.(*corev1.ConfigMap)
		if !ok {
			panic("failed to convert fetched to *corev1.ConfigMap")
		}
		objectModified = configMapDataModified(desiredConfigMap, fetchedConfigMap)
	case *networkingv1.NetworkPolicy:
		desiredNetworkPolicy, ok := desired.(*networkingv1.NetworkPolicy)
		if !ok {
			panic("failed to convert desired to *networkingv1.NetworkPolicy")
		}
		fetchedNetworkPolicy, ok := fetched.(*networkingv1.NetworkPolicy)
		if !ok {
			panic("failed to convert fetched to *networkingv1.NetworkPolicy")
		}
		objectModified = networkPolicySpecModified(desiredNetworkPolicy, fetchedNetworkPolicy)
	default:
		panic(fmt.Sprintf("unsupported object type: %T", desired))
	}
	return objectModified || objectMetadataModified(desired, fetched)
}

func objectMetadataModified(desired, fetched client.Object) bool {
	return !reflect.DeepEqual(desired.GetLabels(), fetched.GetLabels())
}

func certificateSpecModified(desired, fetched *certmanagerv1.Certificate) bool {
	return !reflect.DeepEqual(desired.Spec, fetched.Spec)
}

func deploymentSpecModified(desired, fetched *appsv1.Deployment) bool {
	// check just the fields which are set by the controller and set in static manifest,
	// as fields with default values end up in manifest and causes plain check to fail.
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

func rbacRoleRulesModified[Object *rbacv1.Role | *rbacv1.ClusterRole](desired, fetched Object) bool {
	switch typ := any(desired).(type) {
	case *rbacv1.ClusterRole:
		desiredClusterRole, ok := any(desired).(*rbacv1.ClusterRole)
		if !ok {
			panic("failed to convert desired to *rbacv1.ClusterRole")
		}
		fetchedClusterRole, ok := any(fetched).(*rbacv1.ClusterRole)
		if !ok {
			panic("failed to convert fetched to *rbacv1.ClusterRole")
		}
		return !reflect.DeepEqual(desiredClusterRole.Rules, fetchedClusterRole.Rules)
	case *rbacv1.Role:
		desiredRole, ok := any(desired).(*rbacv1.Role)
		if !ok {
			panic("failed to convert desired to *rbacv1.Role")
		}
		fetchedRole, ok := any(fetched).(*rbacv1.Role)
		if !ok {
			panic("failed to convert fetched to *rbacv1.Role")
		}
		return !reflect.DeepEqual(desiredRole.Rules, fetchedRole.Rules)
	default:
		panic(fmt.Sprintf("unsupported object type %v", typ))
	}
}

func rbacRoleBindingRefModified[Object *rbacv1.RoleBinding | *rbacv1.ClusterRoleBinding](desired, fetched Object) bool {
	switch typ := any(desired).(type) {
	case *rbacv1.ClusterRoleBinding:
		desiredClusterRoleBinding, ok := any(desired).(*rbacv1.ClusterRoleBinding)
		if !ok {
			panic("failed to convert desired to *rbacv1.ClusterRoleBinding")
		}
		fetchedClusterRoleBinding, ok := any(fetched).(*rbacv1.ClusterRoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.ClusterRoleBinding")
		}
		return !reflect.DeepEqual(desiredClusterRoleBinding.RoleRef, fetchedClusterRoleBinding.RoleRef)
	case *rbacv1.RoleBinding:
		desiredRoleBinding, ok := any(desired).(*rbacv1.RoleBinding)
		if !ok {
			panic("failed to convert desired to *rbacv1.RoleBinding")
		}
		fetchedRoleBinding, ok := any(fetched).(*rbacv1.RoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.RoleBinding")
		}
		return !reflect.DeepEqual(desiredRoleBinding.RoleRef, fetchedRoleBinding.RoleRef)
	default:
		panic(fmt.Sprintf("unsupported object type %v", typ))
	}
}

func rbacRoleBindingSubjectsModified[Object *rbacv1.RoleBinding | *rbacv1.ClusterRoleBinding](desired, fetched Object) bool {
	switch typ := any(desired).(type) {
	case *rbacv1.ClusterRoleBinding:
		desiredClusterRoleBinding, ok := any(desired).(*rbacv1.ClusterRoleBinding)
		if !ok {
			panic("failed to convert desired to *rbacv1.ClusterRoleBinding")
		}
		fetchedClusterRoleBinding, ok := any(fetched).(*rbacv1.ClusterRoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.ClusterRoleBinding")
		}
		return !reflect.DeepEqual(desiredClusterRoleBinding.Subjects, fetchedClusterRoleBinding.Subjects)
	case *rbacv1.RoleBinding:
		desiredRoleBinding, ok := any(desired).(*rbacv1.RoleBinding)
		if !ok {
			panic("failed to convert desired to *rbacv1.RoleBinding")
		}
		fetchedRoleBinding, ok := any(fetched).(*rbacv1.RoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.RoleBinding")
		}
		return !reflect.DeepEqual(desiredRoleBinding.Subjects, fetchedRoleBinding.Subjects)
	default:
		panic(fmt.Sprintf("unsupported object type %v", typ))
	}
}

func configMapDataModified(desired, fetched *corev1.ConfigMap) bool {
	return !reflect.DeepEqual(desired.Data, fetched.Data)
}

func networkPolicySpecModified(desired, fetched *networkingv1.NetworkPolicy) bool {
	return !reflect.DeepEqual(desired.Spec, fetched.Spec)
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
		return NewMultipleInstanceError(utilerrors.NewAggregate([]error{fmt.Errorf("%s", statusMessage), updateErr}))
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
		return NewMultipleInstanceError(fmt.Errorf("%s", statusMessage))
	}

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

	return NewMultipleInstanceError(fmt.Errorf("%s", statusMessage))
}
