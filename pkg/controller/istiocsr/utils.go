package istiocsr

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
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
	if err := rbacv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := certmanagerv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

// updateStatus is for updating the status subresource of istiocsr.openshift.operator.io.
func (r *Reconciler) updateStatus(ctx context.Context, changed *v1alpha1.IstioCSR) error {
	namespacedName := types.NamespacedName{Name: changed.Name, Namespace: changed.Namespace}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		r.log.V(1).Info("updating istiocsr.openshift.operator.io status", "request", namespacedName)
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
	namespacedName := types.NamespacedName{Name: istiocsr.Name, Namespace: istiocsr.Namespace}
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
	namespacedName := types.NamespacedName{Name: istiocsr.Name, Namespace: istiocsr.Namespace}
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

func addProcessedAnnotation(istiocsr *v1alpha1.IstioCSR) {
	annotations := istiocsr.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	if _, exist := annotations[controllerProcessedAnnotation]; !exist {
		annotations[controllerProcessedAnnotation] = "true"
	}
	istiocsr.SetAnnotations(annotations)
}

func updateNamespace(obj client.Object, newNamespace string) {
	obj.SetNamespace(newNamespace)
}

func updateResourceLabels(obj client.Object, labels map[string]string) {
	obj.SetLabels(labels)
}

func updateResourceLabelsWithIstioMapperLabels(obj client.Object, istiocsrNamespace string, labels map[string]string) {
	l := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		l[k] = v
	}
	l[istiocsrNamespaceMappingLabelName] = istiocsrNamespace
	obj.SetLabels(l)
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

func hasObjectChanged(desired, fetched client.Object) bool {
	if reflect.TypeOf(desired) != reflect.TypeOf(fetched) {
		panic("both objects to be compared must be of same type")
	}

	var objectModified bool
	switch desired.(type) {
	case *certmanagerv1.Certificate:
		objectModified = certificateSpecModified(desired.(*certmanagerv1.Certificate), fetched.(*certmanagerv1.Certificate))
	case *rbacv1.ClusterRole:
		objectModified = rbacRoleRulesModified[*rbacv1.ClusterRole](desired.(*rbacv1.ClusterRole), fetched.(*rbacv1.ClusterRole))
	case *rbacv1.ClusterRoleBinding:
		objectModified = rbacRoleBindingRefModified[*rbacv1.ClusterRoleBinding](desired.(*rbacv1.ClusterRoleBinding), fetched.(*rbacv1.ClusterRoleBinding)) ||
			rbacRoleBindingSubjectsModified[*rbacv1.ClusterRoleBinding](desired.(*rbacv1.ClusterRoleBinding), fetched.(*rbacv1.ClusterRoleBinding))
	case *appsv1.Deployment:
		objectModified = deploymentSpecModified(desired.(*appsv1.Deployment), fetched.(*appsv1.Deployment))
	case *rbacv1.Role:
		objectModified = rbacRoleRulesModified[*rbacv1.Role](desired.(*rbacv1.Role), fetched.(*rbacv1.Role))
	case *rbacv1.RoleBinding:
		objectModified = rbacRoleBindingRefModified[*rbacv1.RoleBinding](desired.(*rbacv1.RoleBinding), fetched.(*rbacv1.RoleBinding)) ||
			rbacRoleBindingSubjectsModified[*rbacv1.RoleBinding](desired.(*rbacv1.RoleBinding), fetched.(*rbacv1.RoleBinding))
	case *corev1.Service:
		objectModified = serviceSpecModified(desired.(*corev1.Service), fetched.(*corev1.Service))
	case *corev1.ConfigMap:
		objectModified = configMapDataModified(desired.(*corev1.ConfigMap), fetched.(*corev1.ConfigMap))
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

	if !reflect.DeepEqual(desired.Spec.Template.ObjectMeta.Labels, fetched.Spec.Template.ObjectMeta.Labels) ||
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
		return !reflect.DeepEqual(any(desired).(*rbacv1.ClusterRole).Rules, any(fetched).(*rbacv1.ClusterRole).Rules)
	case *rbacv1.Role:
		return !reflect.DeepEqual(any(desired).(*rbacv1.Role).Rules, any(fetched).(*rbacv1.Role).Rules)
	default:
		panic(fmt.Sprintf("unsupported object type %v", typ))
	}
}

func rbacRoleBindingRefModified[Object *rbacv1.RoleBinding | *rbacv1.ClusterRoleBinding](desired, fetched Object) bool {
	switch typ := any(desired).(type) {
	case *rbacv1.ClusterRoleBinding:
		return !reflect.DeepEqual(any(desired).(*rbacv1.ClusterRoleBinding).RoleRef, any(fetched).(*rbacv1.ClusterRoleBinding).RoleRef)
	case *rbacv1.RoleBinding:
		return !reflect.DeepEqual(any(desired).(*rbacv1.RoleBinding).RoleRef, any(fetched).(*rbacv1.RoleBinding).RoleRef)
	default:
		panic(fmt.Sprintf("unsupported object type %v", typ))
	}
}

func rbacRoleBindingSubjectsModified[Object *rbacv1.RoleBinding | *rbacv1.ClusterRoleBinding](desired, fetched Object) bool {
	switch typ := any(desired).(type) {
	case *rbacv1.ClusterRoleBinding:
		return !reflect.DeepEqual(any(desired).(*rbacv1.ClusterRoleBinding).Subjects, any(fetched).(*rbacv1.ClusterRoleBinding).Subjects)
	case *rbacv1.RoleBinding:
		return !reflect.DeepEqual(any(desired).(*rbacv1.RoleBinding).Subjects, any(fetched).(*rbacv1.RoleBinding).Subjects)
	default:
		panic(fmt.Sprintf("unsupported object type %v", typ))
	}
}

func configMapDataModified(desired, fetched *corev1.ConfigMap) bool {
	return !reflect.DeepEqual(desired.Data, fetched.Data)
}
