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

var (
	errAddFinalizerFailed     = errors.New("failed to add finalizers to istiocsr.openshift.operator.io object")
	errRemoveFinalizerFailed  = errors.New("failed to remove finalizers from istiocsr.openshift.operator.io object")
	errIstioCSRConfigEmpty    = errors.New("spec.istioCSRConfig config cannot be empty")
	errIstiodTLSConfigEmpty   = errors.New("spec.istioCSRConfig.istiodTLSConfig config cannot be empty")
	errIstioConfigEmpty       = errors.New("spec.istioCSRConfig.istio config cannot be empty")
	errCertManagerConfigEmpty = errors.New("spec.istioCSRConfig.certManager config cannot be empty")
	errMultipleISRInstances   = errors.New("multiple instances of istiocsr exist")
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
			return fmt.Errorf("%q: %w", namespacedName, errAddFinalizerFailed)
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
			return fmt.Errorf("%q: %w", namespacedName, errRemoveFinalizerFailed)
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

// applyResource handles the common create-or-update logic for any Kubernetes resource.
// It checks if the resource exists and either creates it or updates it to match the desired state.
// resourceKind is used in log and event messages (e.g. "clusterrole", "deployment").
func (r *Reconciler) applyResource(istiocsr *v1alpha1.IstioCSR, desired, fetched client.Object, resourceName, resourceKind string, exist, istioCSRCreateRecon bool) error {
	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s %s resource already exists, maybe from previous installation", resourceName, resourceKind)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info(resourceKind+" has been modified, updating to desired state", "name", resourceName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return common.FromClientError(err, "failed to update %s %s resource", resourceName, resourceKind)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "%s resource %s reconciled back to desired state", resourceKind, resourceName)
	} else {
		r.log.V(4).Info(resourceKind+" resource already exists and is in expected state", "name", resourceName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return common.FromClientError(err, "failed to create %s %s resource", resourceName, resourceKind)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "%s resource %s created", resourceKind, resourceName)
	}
	return nil
}

func hasObjectChanged(desired, fetched client.Object) bool {
	if reflect.TypeOf(desired) != reflect.TypeOf(fetched) {
		panic("both objects to be compared must be of same type")
	}
	return isSpecModified(desired, fetched) || common.ObjectMetadataModified(desired, fetched)
}

// isRBACModified checks if any RBAC-typed object (ClusterRole, ClusterRoleBinding, Role,
// RoleBinding) has been modified. Types are guaranteed to match by the caller.
func isRBACModified(desired, fetched client.Object) bool {
	switch desiredObj := desired.(type) {
	case *rbacv1.ClusterRole:
		fetchedObj, ok := fetched.(*rbacv1.ClusterRole)
		if !ok {
			panic(fmt.Sprintf("fetched object type mismatch: expected %T, got %T", desired, fetched))
		}
		return rbacRoleRulesModified(desiredObj, fetchedObj)
	case *rbacv1.ClusterRoleBinding:
		fetchedObj, ok := fetched.(*rbacv1.ClusterRoleBinding)
		if !ok {
			panic(fmt.Sprintf("fetched object type mismatch: expected %T, got %T", desired, fetched))
		}
		return rbacRoleBindingRefModified(desiredObj, fetchedObj) || rbacRoleBindingSubjectsModified(desiredObj, fetchedObj)
	case *rbacv1.Role:
		fetchedObj, ok := fetched.(*rbacv1.Role)
		if !ok {
			panic(fmt.Sprintf("fetched object type mismatch: expected %T, got %T", desired, fetched))
		}
		return rbacRoleRulesModified(desiredObj, fetchedObj)
	case *rbacv1.RoleBinding:
		fetchedObj, ok := fetched.(*rbacv1.RoleBinding)
		if !ok {
			panic(fmt.Sprintf("fetched object type mismatch: expected %T, got %T", desired, fetched))
		}
		return rbacRoleBindingRefModified(desiredObj, fetchedObj) || rbacRoleBindingSubjectsModified(desiredObj, fetchedObj)
	default:
		panic(fmt.Sprintf("unsupported RBAC object type: %T", desired))
	}
}

// isNonRBACModified checks if a non-RBAC object has been modified.
// Types are guaranteed to match by the caller.
func isNonRBACModified(desired, fetched client.Object) bool {
	switch desiredObj := desired.(type) {
	case *certmanagerv1.Certificate:
		fetchedObj, ok := fetched.(*certmanagerv1.Certificate)
		if !ok {
			panic(fmt.Sprintf("fetched object type mismatch: expected %T, got %T", desired, fetched))
		}
		return certificateSpecModified(desiredObj, fetchedObj)
	case *appsv1.Deployment:
		fetchedObj, ok := fetched.(*appsv1.Deployment)
		if !ok {
			panic(fmt.Sprintf("fetched object type mismatch: expected %T, got %T", desired, fetched))
		}
		return deploymentSpecModified(desiredObj, fetchedObj)
	case *corev1.Service:
		fetchedObj, ok := fetched.(*corev1.Service)
		if !ok {
			panic(fmt.Sprintf("fetched object type mismatch: expected %T, got %T", desired, fetched))
		}
		return serviceSpecModified(desiredObj, fetchedObj)
	case *corev1.ConfigMap:
		fetchedObj, ok := fetched.(*corev1.ConfigMap)
		if !ok {
			panic(fmt.Sprintf("fetched object type mismatch: expected %T, got %T", desired, fetched))
		}
		return configMapDataModified(desiredObj, fetchedObj)
	case *networkingv1.NetworkPolicy:
		fetchedObj, ok := fetched.(*networkingv1.NetworkPolicy)
		if !ok {
			panic(fmt.Sprintf("fetched object type mismatch: expected %T, got %T", desired, fetched))
		}
		return networkPolicySpecModified(desiredObj, fetchedObj)
	default:
		panic(fmt.Sprintf("unsupported object type: %T", desired))
	}
}

// isSpecModified dispatches spec comparison to the appropriate type-specific helper.
func isSpecModified(desired, fetched client.Object) bool {
	switch desired.(type) {
	case *rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding, *rbacv1.Role, *rbacv1.RoleBinding:
		return isRBACModified(desired, fetched)
	default:
		return isNonRBACModified(desired, fetched)
	}
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

	if containerSpecModified(desired.Spec.Template.Spec.Containers[0], fetched.Spec.Template.Spec.Containers[0]) {
		return true
	}

	if desired.Spec.Template.Spec.ServiceAccountName != fetched.Spec.Template.Spec.ServiceAccountName ||
		!reflect.DeepEqual(desired.Spec.Template.Spec.NodeSelector, fetched.Spec.Template.Spec.NodeSelector) ||
		!reflect.DeepEqual(desired.Spec.Template.Spec.Volumes, fetched.Spec.Template.Spec.Volumes) {
		return true
	}

	return false
}

func containerSpecModified(desired, fetched corev1.Container) bool {
	if containerBasicPropsModified(desired, fetched) {
		return true
	}
	if portsModified(desired.Ports, fetched.Ports) {
		return true
	}
	if containerReadinessProbeModified(desired, fetched) {
		return true
	}
	return containerResourcesModified(desired, fetched)
}

func containerBasicPropsModified(desired, fetched corev1.Container) bool {
	return !reflect.DeepEqual(desired.Args, fetched.Args) ||
		desired.Name != fetched.Name || desired.Image != fetched.Image ||
		desired.ImagePullPolicy != fetched.ImagePullPolicy
}

func portsModified(desired, fetched []corev1.ContainerPort) bool {
	if len(desired) != len(fetched) {
		return true
	}
	for _, fetchedPort := range fetched {
		matched := false
		for _, desiredPort := range desired {
			if fetchedPort.ContainerPort == desiredPort.ContainerPort {
				matched = true
				break
			}
		}
		if !matched {
			return true
		}
	}
	return false
}

func containerReadinessProbeModified(desired, fetched corev1.Container) bool {
	return desired.ReadinessProbe.HTTPGet.Path != fetched.ReadinessProbe.HTTPGet.Path ||
		desired.ReadinessProbe.InitialDelaySeconds != fetched.ReadinessProbe.InitialDelaySeconds ||
		desired.ReadinessProbe.PeriodSeconds != fetched.ReadinessProbe.PeriodSeconds
}

func containerResourcesModified(desired, fetched corev1.Container) bool {
	return !reflect.DeepEqual(desired.Resources, fetched.Resources) ||
		!reflect.DeepEqual(*desired.SecurityContext, *fetched.SecurityContext) ||
		!reflect.DeepEqual(desired.VolumeMounts, fetched.VolumeMounts)
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
		return errIstioCSRConfigEmpty
	}
	if reflect.ValueOf(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig).IsZero() {
		return errIstiodTLSConfigEmpty
	}
	if reflect.ValueOf(istiocsr.Spec.IstioCSRConfig.Istio).IsZero() {
		return errIstioConfigEmpty
	}
	if reflect.ValueOf(istiocsr.Spec.IstioCSRConfig.CertManager).IsZero() {
		return errCertManagerConfigEmpty
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

// isOldestInstance returns true if istiocsr is the oldest (or only) instance among all listed items.
func isOldestInstance(istiocsr *v1alpha1.IstioCSR, items []v1alpha1.IstioCSR) bool {
	for _, item := range items {
		if item.GetNamespace() == istiocsr.Namespace {
			continue
		}
		// Even when timestamps are equal we treat this instance as not the oldest.
		// If all instances were created in parallel, onus is on the user to delete
		// all and recreate just one required instance of istiocsr.
		if item.CreationTimestamp.Time.Before(istiocsr.CreationTimestamp.Time) ||
			item.CreationTimestamp.Time.Equal(istiocsr.CreationTimestamp.Time) {
			return false
		}
	}
	return true
}

// rejectAlreadyMarkedInstance handles an istiocsr that already has the processing-rejected annotation.
func (r *Reconciler) rejectAlreadyMarkedInstance(istiocsr *v1alpha1.IstioCSR, statusMessage string) error {
	r.log.V(4).Info("%s/%s istiocsr resource contains processing rejected annotation", istiocsr.Namespace, istiocsr.Name)
	var updateErr error
	if istiocsr.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonFailed, statusMessage) {
		updateErr = r.updateCondition(istiocsr, nil)
	}
	return common.NewMultipleInstanceError(utilerrors.NewAggregate([]error{fmt.Errorf("%s: %w", statusMessage, errMultipleISRInstances), updateErr}))
}

// rejectInstance marks this instance as rejected and returns a multiple-instance error.
func (r *Reconciler) rejectInstance(istiocsr *v1alpha1.IstioCSR, statusMessage string) error {
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
	return common.NewMultipleInstanceError(fmt.Errorf("%s: %w", statusMessage, errMultipleISRInstances))
}

func (r *Reconciler) disallowMultipleIstioCSRInstances(istiocsr *v1alpha1.IstioCSR) error {
	statusMessage := fmt.Sprintf("multiple instances of istiocsr exists, %s/%s will not be processed", istiocsr.GetNamespace(), istiocsr.GetName())

	if containsProcessingRejectedAnnotation(istiocsr) {
		return r.rejectAlreadyMarkedInstance(istiocsr, statusMessage)
	}

	istiocsrList := &v1alpha1.IstioCSRList{}
	if err := r.List(r.ctx, istiocsrList); err != nil {
		return fmt.Errorf("failed to fetch list of istiocsr resources: %w", err)
	}

	if len(istiocsrList.Items) <= 1 {
		return nil
	}

	if isOldestInstance(istiocsr, istiocsrList.Items) {
		// This is the oldest instance, allow it to proceed
		return nil
	}

	// This instance should be rejected as there's an older or equally old instance
	return r.rejectInstance(istiocsr, statusMessage)
}
