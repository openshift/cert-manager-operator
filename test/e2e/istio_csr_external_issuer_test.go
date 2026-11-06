//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	testistiocsr "github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

const (
	istioCSRExternalIssuerClusterRoleName = "cert-manager-operator-istiocsr-external-issuer"
	googleCASIssuerName                   = "google-cas-issuer"
	googleCASIssuerGroup                  = "cas-issuer.jetstack.io"
)

var googleCASIssuerGVR = schema.GroupVersionResource{
	Group:    googleCASIssuerGroup,
	Version:  "v1",
	Resource: "googlecasissuers",
}

func operatorServiceAccountName(ctx context.Context, clientset *kubernetes.Clientset) string {
	deployment, err := clientset.AppsV1().Deployments(operatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	if deployment.Spec.Template.Spec.ServiceAccountName != "" {
		return deployment.Spec.Template.Spec.ServiceAccountName
	}
	return "default"
}

func ensureIstioCSRExternalIssuerClusterRole(ctx context.Context) {
	By("ensuring optional IstioCSR external issuer ClusterRole exists")
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istiocsr_external_issuer_clusterrole.yaml"), "")
	_ = ctx
}

func bindIstioCSRExternalIssuerClusterRole(ctx context.Context, clientset *kubernetes.Clientset, bindingName string) func() {
	ensureIstioCSRExternalIssuerClusterRole(ctx)

	saName := operatorServiceAccountName(ctx, clientset)
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: bindingName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     istioCSRExternalIssuerClusterRoleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      saName,
				Namespace: operatorNamespace,
			},
		},
	}

	By(fmt.Sprintf("binding %q to operator service account %s/%s", istioCSRExternalIssuerClusterRoleName, operatorNamespace, saName))
	_, err := clientset.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}

	return func() {
		_ = clientset.RbacV1().ClusterRoleBindings().Delete(context.Background(), bindingName, metav1.DeleteOptions{})
	}
}

func ensureGoogleCASIssuerCRD(ctx context.Context) {
	By("ensuring GoogleCASIssuer CRD exists for external issuer e2e")
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "google_cas_issuer_crd.yaml"), "")
	_ = ctx
}

func createGoogleCASIssuer(namespace string) {
	By("creating GoogleCASIssuer in istio namespace")
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "google_cas_issuer.yaml"), namespace)
	DeferCleanup(func() {
		_ = loader.DynamicClient.Resource(googleCASIssuerGVR).Namespace(namespace).Delete(context.Background(), googleCASIssuerName, metav1.DeleteOptions{})
	})
}

func expectGoogleCASIssuerWatchLabel(ctx context.Context, istioCSRNamespace string) {
	Eventually(func(g Gomega) {
		obj, err := loader.DynamicClient.Resource(googleCASIssuerGVR).Namespace(istioCSRNamespace).Get(ctx, googleCASIssuerName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		labels := obj.GetLabels()
		g.Expect(labels).NotTo(BeNil())
		g.Expect(labels).To(HaveKey(testistiocsr.IstiocsrResourceWatchLabelName))
		g.Expect(labels[testistiocsr.IstiocsrResourceWatchLabelName]).To(ContainSubstring(istioCSRNamespace))
	}).WithTimeout(highTimeout).WithPolling(slowPollInterval).Should(Succeed())
}

func unstructuredIssuerRefSpec(obj *unstructured.Unstructured) map[string]interface{} {
	spec, found, err := unstructured.NestedMap(obj.Object, "spec", "istioCSRConfig", "certManager", "issuerRef")
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue())
	return spec
}
