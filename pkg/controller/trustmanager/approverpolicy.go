package trustmanager

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

var certificateRequestPolicyGVK = schema.GroupVersionKind{
	Group:   "policy.cert-manager.io",
	Version: "v1alpha1",
	Kind:    "CertificateRequestPolicy",
}

// createOrApplyApproverPolicyResources creates the webhook CertificateRequestPolicy
// and the RBAC that lets the cert-manager ServiceAccount use it, matching upstream
// Helm app.webhook.tls.approverPolicy. Nothing is created unless policy is Enabled.
// If Enabled and the CertificateRequestPolicy CRD is missing (approver-policy not
// installed), reconciliation fails with a CRD-missing error.
func (r *Reconciler) createOrApplyApproverPolicyResources(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	if !approverPolicyEnabled(trustManager.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy) {
		return nil
	}

	if err := r.createOrApplyCertificateRequestPolicy(trustManager, resourceLabels, resourceAnnotations); err != nil {
		return err
	}
	if err := r.createOrApplyPolicyClusterRole(trustManager, resourceLabels, resourceAnnotations); err != nil {
		return err
	}
	if err := r.createOrApplyPolicyClusterRoleBinding(trustManager, resourceLabels, resourceAnnotations); err != nil {
		return err
	}
	return nil
}

// createOrApplyCertificateRequestPolicy applies CertificateRequestPolicy
// trust-manager-policy so approver-policy can auto-approve the webhook cert.
// A missing CRP CRD is treated as a reconcile error (install approver-policy
// or set policy to Disabled).
func (r *Reconciler) createOrApplyCertificateRequestPolicy(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getCertificateRequestPolicyObject(resourceLabels, resourceAnnotations)
	resourceName := desired.GetName()
	r.log.V(4).Info("reconciling certificaterequestpolicy resource", "name", resourceName)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(certificateRequestPolicyGVK)
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return common.FromClientError(err, "CertificateRequestPolicy CRD is not installed; install cert-manager-approver-policy or set spec.trustManagerConfig.webhookTLS.approverPolicy.policy to Disabled")
		}
		return common.FromClientError(err, "failed to check if certificaterequestpolicy %q exists", resourceName)
	}
	if exists && !certificateRequestPolicyModified(desired, existing) {
		r.log.V(4).Info("certificaterequestpolicy resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("certificaterequestpolicy resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		if meta.IsNoMatchError(err) {
			return common.FromClientError(err, "CertificateRequestPolicy CRD is not installed; install cert-manager-approver-policy or set spec.trustManagerConfig.webhookTLS.approverPolicy.policy to Disabled")
		}
		return common.FromClientError(err, "failed to apply certificaterequestpolicy %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "certificaterequestpolicy resource %s applied", resourceName)
	return nil
}

func getCertificateRequestPolicyObject(resourceLabels, resourceAnnotations map[string]string) *unstructured.Unstructured {
	dnsName := fmt.Sprintf("%s.%s.svc", trustManagerServiceName, operandNamespace)
	obj := &unstructured.Unstructured{Object: map[string]interface{}{}}
	obj.SetGroupVersionKind(certificateRequestPolicyGVK)
	obj.SetName(trustManagerCertificateRequestPolicyName)
	common.UpdateResourceLabels(obj, resourceLabels)
	updateResourceAnnotations(obj, resourceAnnotations)
	obj.Object["spec"] = map[string]interface{}{
		"allowed": map[string]interface{}{
			"commonName": map[string]interface{}{
				"value":    dnsName,
				"required": true,
			},
			"dnsNames": map[string]interface{}{
				"values":   []interface{}{dnsName},
				"required": true,
			},
		},
		"selector": map[string]interface{}{
			"issuerRef": map[string]interface{}{
				"name":  trustManagerIssuerName,
				"kind":  "Issuer",
				"group": "cert-manager.io",
			},
		},
	}
	return obj
}

func certificateRequestPolicyModified(desired, existing *unstructured.Unstructured) bool {
	return managedMetadataModified(desired, existing) ||
		!reflect.DeepEqual(desired.Object["spec"], existing.Object["spec"])
}

func (r *Reconciler) createOrApplyPolicyClusterRole(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getPolicyClusterRoleObject(resourceLabels, resourceAnnotations)
	resourceName := desired.GetName()
	r.log.V(4).Info("reconciling approver-policy clusterrole resource", "name", resourceName)

	existing := &rbacv1.ClusterRole{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if clusterrole %q exists", resourceName)
	}
	if exists && !clusterRoleModified(desired, existing) {
		r.log.V(4).Info("approver-policy clusterrole resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("approver-policy clusterrole resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply clusterrole %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "clusterrole resource %s applied", resourceName)
	return nil
}

// getPolicyClusterRoleObject returns ClusterRole trust-manager-policy-role,
// granting the cert-manager ServiceAccount use of CertificateRequestPolicy trust-manager-policy.
func getPolicyClusterRoleObject(resourceLabels, resourceAnnotations map[string]string) *rbacv1.ClusterRole {
	role := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: trustManagerPolicyClusterRoleName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"policy.cert-manager.io"},
				Resources:     []string{"certificaterequestpolicies"},
				Verbs:         []string{"use"},
				ResourceNames: []string{trustManagerCertificateRequestPolicyName},
			},
		},
	}
	common.UpdateResourceLabels(role, resourceLabels)
	updateResourceAnnotations(role, resourceAnnotations)
	return role
}

func (r *Reconciler) createOrApplyPolicyClusterRoleBinding(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getPolicyClusterRoleBindingObject(resourceLabels, resourceAnnotations)
	resourceName := desired.GetName()
	r.log.V(4).Info("reconciling approver-policy clusterrolebinding resource", "name", resourceName)

	existing := &rbacv1.ClusterRoleBinding{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if clusterrolebinding %q exists", resourceName)
	}
	if exists && !clusterRoleBindingModified(desired, existing) {
		r.log.V(4).Info("approver-policy clusterrolebinding resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("approver-policy clusterrolebinding resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply clusterrolebinding %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "clusterrolebinding resource %s applied", resourceName)
	return nil
}

// getPolicyClusterRoleBindingObject binds trust-manager-policy-role to the
// cert-manager ServiceAccount in the operand namespace.
func getPolicyClusterRoleBindingObject(resourceLabels, resourceAnnotations map[string]string) *rbacv1.ClusterRoleBinding {
	binding := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: trustManagerPolicyClusterRoleBindingName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     trustManagerPolicyClusterRoleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      roleBindingSubjectKind,
				Name:      certManagerControllerServiceAccountName,
				Namespace: operandNamespace,
			},
		},
	}
	common.UpdateResourceLabels(binding, resourceLabels)
	updateResourceAnnotations(binding, resourceAnnotations)
	return binding
}
