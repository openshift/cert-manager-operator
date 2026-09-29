package trustmanager

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	policyv1alpha1 "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// createOrApplyApproverPolicyResources creates the webhook CertificateRequestPolicy
// and the RBAC that lets the cert-manager ServiceAccount use it, matching upstream
// Helm app.webhook.tls.approverPolicy. Resources are created when bootstrapResources
// is Enabled and deleted when it is not. If Enabled and the CertificateRequestPolicy
// CRD is missing (approver-policy not installed), reconciliation fails with a
// CRD-missing error. A missing CRD is ignored while bootstrapResources is Disabled.
func (r *Reconciler) createOrApplyApproverPolicyResources(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	if !approverPolicyEnabled(trustManager.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy) {
		return r.deleteApproverPolicyResources(trustManager)
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

// deleteApproverPolicyResources removes the operator-created CertificateRequestPolicy
// and the RBAC that grants use of it. NotFound is success. A missing
// CertificateRequestPolicy CRD is ignored so Disabled remains valid when
// approver-policy is not installed.
func (r *Reconciler) deleteApproverPolicyResources(trustManager *v1alpha1.TrustManager) error {
	if err := r.deleteApproverPolicyObject(trustManager, &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{Name: trustManagerPolicyClusterRoleBindingName},
	}, "clusterrolebinding", false); err != nil {
		return err
	}
	if err := r.deleteApproverPolicyObject(trustManager, &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{Name: trustManagerPolicyClusterRoleName},
	}, "clusterrole", false); err != nil {
		return err
	}
	return r.deleteApproverPolicyObject(trustManager, &policyv1alpha1.CertificateRequestPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: policyv1alpha1.SchemeGroupVersion.String(),
			Kind:       policyv1alpha1.CertificateRequestPolicyKind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: trustManagerCertificateRequestPolicyName},
	}, "certificaterequestpolicy", true)
}

func (r *Reconciler) deleteApproverPolicyObject(trustManager *v1alpha1.TrustManager, obj client.Object, resourceKind string, ignoreNoMatch bool) error {
	resourceName := obj.GetName()
	r.log.V(4).Info("ensuring approver-policy resource is absent", "kind", resourceKind, "name", resourceName)
	if err := r.Delete(r.ctx, obj); err != nil {
		if apierrors.IsNotFound(err) || (ignoreNoMatch && meta.IsNoMatchError(err)) {
			return nil
		}
		return common.FromClientError(err, "failed to delete %s %q", resourceKind, resourceName)
	}
	r.log.V(2).Info("deleted approver-policy resource", "kind", resourceKind, "name", resourceName)
	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "%s resource %s deleted", resourceKind, resourceName)
	return nil
}

// createOrApplyCertificateRequestPolicy applies CertificateRequestPolicy
// trust-manager-policy so approver-policy can auto-approve the webhook cert.
// A missing CRP CRD is treated as a reconcile error (install approver-policy
// or set bootstrapResources to Disabled).
func (r *Reconciler) createOrApplyCertificateRequestPolicy(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getCertificateRequestPolicyObject(resourceLabels, resourceAnnotations)
	resourceName := desired.GetName()
	r.log.V(4).Info("reconciling certificaterequestpolicy resource", "name", resourceName)

	existing := &policyv1alpha1.CertificateRequestPolicy{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return common.FromClientError(err, "CertificateRequestPolicy CRD is not installed; install cert-manager-approver-policy or set spec.trustManagerConfig.webhookTLS.approverPolicy.bootstrapResources to Disabled")
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
			return common.FromClientError(err, "CertificateRequestPolicy CRD is not installed; install cert-manager-approver-policy or set spec.trustManagerConfig.webhookTLS.approverPolicy.bootstrapResources to Disabled")
		}
		return common.FromClientError(err, "failed to apply certificaterequestpolicy %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "certificaterequestpolicy resource %s applied", resourceName)
	return nil
}

func getCertificateRequestPolicyObject(resourceLabels, resourceAnnotations map[string]string) *policyv1alpha1.CertificateRequestPolicy {
	dnsName := fmt.Sprintf("%s.%s.svc", trustManagerServiceName, operandNamespace)
	// cert-manager defaults an unset Certificate spec.usages to these two values.
	// approver-policy treats an omitted allowed.usages list as permitting none.
	usages := []certmanagerv1.KeyUsage{certmanagerv1.UsageDigitalSignature, certmanagerv1.UsageKeyEncipherment}
	obj := &policyv1alpha1.CertificateRequestPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: policyv1alpha1.SchemeGroupVersion.String(),
			Kind:       policyv1alpha1.CertificateRequestPolicyKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: trustManagerCertificateRequestPolicyName,
		},
		Spec: policyv1alpha1.CertificateRequestPolicySpec{
			Allowed: &policyv1alpha1.CertificateRequestPolicyAllowed{
				CommonName: &policyv1alpha1.CertificateRequestPolicyAllowedString{
					Value:    ptr.To(dnsName),
					Required: ptr.To(true),
				},
				DNSNames: &policyv1alpha1.CertificateRequestPolicyAllowedStringSlice{
					Values:   ptr.To([]string{dnsName}),
					Required: ptr.To(true),
				},
				Usages: &usages,
			},
			Selector: policyv1alpha1.CertificateRequestPolicySelector{
				IssuerRef: &policyv1alpha1.CertificateRequestPolicySelectorIssuerRef{
					Name:  ptr.To(trustManagerIssuerName),
					Kind:  ptr.To("Issuer"),
					Group: ptr.To("cert-manager.io"),
				},
			},
		},
	}
	common.UpdateResourceLabels(obj, resourceLabels)
	updateResourceAnnotations(obj, resourceAnnotations)
	return obj
}

func certificateRequestPolicyModified(desired, existing *policyv1alpha1.CertificateRequestPolicy) bool {
	return managedMetadataModified(desired, existing) ||
		!reflect.DeepEqual(desired.Spec, existing.Spec)
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
