package istiocsr

import (
	"context"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr/testr"

	cmacme "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	"github.com/openshift/cert-manager-operator/test/library"
)

const (
	testResourcesName     = "istiocsr-test-resource"
	testIstioCSRNamespace = "istiocsr-test-ns"
	testIstiodNamespace   = "istio-test-ns"
	image                 = "registry.redhat.io/cert-manager/cert-manager-istio-csr-rhel9:latest"
)

var (
	testError = fmt.Errorf("test client error")
)

func testReconciler(t *testing.T) *Reconciler {
	return &Reconciler{
		ctx:           context.Background(),
		eventRecorder: record.NewFakeRecorder(100),
		log:           testr.New(t),
		scheme:        library.Scheme,
	}
}

func testIstioCSR() *v1alpha1.IstioCSR {
	return &v1alpha1.IstioCSR{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testResourcesName,
			Namespace: testIstioCSRNamespace,
		},
		Spec: v1alpha1.IstioCSRSpec{
			IstioCSRConfig: &v1alpha1.IstioCSRConfig{
				CertManager: &v1alpha1.CertManagerConfig{
					IssuerRef: certmanagermetav1.ObjectReference{
						Name:  testResourcesName,
						Kind:  issuerKind,
						Group: issuerGroup,
					},
				},
				IstiodTLSConfig: &v1alpha1.IstiodTLSConfig{
					CertificateDuration:    &metav1.Duration{Duration: time.Hour},
					CertificateRenewBefore: &metav1.Duration{Duration: time.Minute * 30},
					MaxCertificateDuration: &metav1.Duration{Duration: time.Hour},
					PrivateKeySize:         DefaultRSAPrivateKeySize,
					SignatureAlgorithm:     string(DefaultSignatureAlgorithm),
					TrustDomain:            "cluster.local",
				},
				Istio: &v1alpha1.IstioConfig{
					Namespace: testIstiodNamespace,
					Revisions: []string{"default"},
				},
				LogLevel:  1,
				LogFormat: "text",
			},
			ControllerConfig: &v1alpha1.ControllerConfig{
				Labels: map[string]string{
					"user-label1": "test",
					"user-label2": "test",
				},
			},
		},
	}
}

func testIssuer() *certmanagerv1.Issuer {
	return &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testResourcesName,
			Namespace: testIstiodNamespace,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{
					SecretName: testResourcesName,
				},
			},
		},
	}
}

func testClusterIssuer() *certmanagerv1.ClusterIssuer {
	return &certmanagerv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{
			Name: testResourcesName,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{
					SecretName: testResourcesName,
				},
			},
		},
	}
}

func testACMEIssuer() *certmanagerv1.ClusterIssuer {
	return &certmanagerv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{
			Name: testResourcesName,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				ACME: &cmacme.ACMEIssuer{
					Email: "test@example.com",
				},
			},
		},
	}
}

func testCertificate() *certmanagerv1.Certificate {
	cert := decodeCertificateObjBytes(assets.MustAsset(certificateAssetName))
	cert.SetNamespace(testIstiodNamespace)
	labels := make(map[string]string)
	for k, v := range controllerDefaultResourceLabels {
		labels[k] = v
	}
	labels[istiocsrNamespaceMappingLabelName] = testIstioCSRNamespace
	cert.SetLabels(labels)
	cert.Spec.CommonName = fmt.Sprintf("istiod.%s.svc", testIstiodNamespace)
	cert.Spec.DNSNames = []string{fmt.Sprintf("istiod.%s.svc", testIstiodNamespace)}
	cert.Spec.URIs = []string{
		fmt.Sprintf(istiodCertificateSpiffeURIFmt, "cluster.local", testIstiodNamespace),
	}
	cert.Spec.IssuerRef.Name = testResourcesName
	return cert
}

func testClusterRole() *rbacv1.ClusterRole {
	role := decodeClusterRoleObjBytes(assets.MustAsset(clusterRoleAssetName))
	role.SetName("cert-manager-istio-csr-sdghj")
	role.SetLabels(controllerDefaultResourceLabels)
	return role
}

func testClusterRoleBinding() *rbacv1.ClusterRoleBinding {
	roleBinding := decodeClusterRoleBindingObjBytes(assets.MustAsset(clusterRoleBindingAssetName))
	roleBinding.SetName("cert-manager-istio-csr-dfkhk")
	roleBinding.SetGenerateName("cert-manager-istio-csr-")
	roleBinding.SetLabels(controllerDefaultResourceLabels)
	return roleBinding
}

func testClusterRoleBindingExtra() *rbacv1.ClusterRoleBinding {
	roleBinding := decodeClusterRoleBindingObjBytes(assets.MustAsset(clusterRoleBindingAssetName))
	roleBinding.SetName("cert-manager-istio-csr-dfmfj")
	roleBinding.SetGenerateName("cert-manager-istio-csr-")
	roleBinding.SetLabels(controllerDefaultResourceLabels)
	return roleBinding
}

func testDeployment() *appsv1.Deployment {
	deployment := decodeDeploymentObjBytes(assets.MustAsset(deploymentAssetName))
	deployment.SetNamespace(testIstioCSRNamespace)
	deployment.SetLabels(controllerDefaultResourceLabels)
	deployment.Spec.Template.Labels = controllerDefaultResourceLabels
	deployment.Spec.Template.Spec.Containers[0].Image = image
	return deployment
}

func testRole() *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleAssetName))
	role.SetNamespace(testIstiodNamespace)
	role.SetLabels(controllerDefaultResourceLabels)
	return role
}

func testRoleBinding() *rbacv1.RoleBinding {
	roleBinding := decodeRoleBindingObjBytes(assets.MustAsset(roleBindingAssetName))
	roleBinding.SetNamespace(testIstiodNamespace)
	roleBinding.SetLabels(controllerDefaultResourceLabels)
	return roleBinding
}

func testRoleLeases() *rbacv1.Role {
	role := decodeRoleObjBytes(assets.MustAsset(roleLeasesAssetName))
	role.SetNamespace(testIstiodNamespace)
	role.SetLabels(controllerDefaultResourceLabels)
	return role
}

func testRoleBindingLeases() *rbacv1.RoleBinding {
	roleBinding := decodeRoleBindingObjBytes(assets.MustAsset(roleBindingLeasesAssetName))
	roleBinding.SetNamespace(testIstiodNamespace)
	roleBinding.SetLabels(controllerDefaultResourceLabels)
	return roleBinding
}

func testService() *corev1.Service {
	service := decodeServiceObjBytes(assets.MustAsset(serviceAssetName))
	service.SetNamespace(testIstioCSRNamespace)
	service.SetLabels(controllerDefaultResourceLabels)
	return service
}

func testServiceAccount() *corev1.ServiceAccount {
	serviceAccount := decodeServiceAccountObjBytes(assets.MustAsset(serviceAccountAssetName))
	serviceAccount.SetNamespace(testIstioCSRNamespace)
	serviceAccount.SetLabels(controllerDefaultResourceLabels)
	return serviceAccount
}

func testConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testResourcesName,
			Namespace: testIstioCSRNamespace,
		},
		Data: map[string]string{
			istiocsrCAKeyName: "testCAData",
		},
	}
}

func testCACertificateConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca-cert-test",
			Namespace: testIstioCSRNamespace, // Use IstioCSR namespace by default
		},
		Data: map[string]string{
			"ca-cert.pem": `-----BEGIN CERTIFICATE-----
MIIDQTCCAimgAwIBAgITBmyfz5m/jAo54vB4ikPmljZbyjANBgkqhkiG9w0BAQsF
ADA5MQswCQYDVQQGEwJVUzEPMA0GA1UEChMGQW1hem9uMRkwFwYDVQQDExBBbWF6
b24gUm9vdCBDQSAxMB4XDTE1MDUyNjAwMDAwMFoXDTM4MDExNzAwMDAwMFowOTEL
MAkGA1UEBhMCVVMxDzANBgNVBAoTBkFtYXpvbjEZMBcGA1UEAxMQQW1hem9uIFJv
b3QgQ0EgMTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALJ4gHHKeNXj
ca9HgFB0fW7Y14h29Jlo91ghYPl0hAEvrAIthtOgQ3pOsqTQNroBvo3bSMgHFzZM
9O6II8c+6zf1tRn4SWiw3te5djgdYZ6k/oI2peVKVuRF4fn9tBb6dNqcmzU5L/qw
IFAGbHrQgLKm+a/sRxmPUDgH3KKHOVj4utWp+UhnMJbulHheb4mjUcAwhmahRWa6
VOujw5H5SNz/0egwLX0tdHA114gk957EWW67c4cX8jJGKLhD+rcdqsq08p8kDi1L
93FcXmn/6pUCyziKrlA4b9v7LWIbxcceVOF34GfID5yHI9Y/QCB/IIDEgEw+OyQm
jgSubJrIqg0CAwEAAaNCMEAwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMC
AYYwHQYDVR0OBBYEFIQYzIU07LwMlJQuCFmcx7IQTgoIMA0GCSqGSIb3DQEBCwUA
A4IBAQCY8jdaQZChGsV2USggNiMOruYou6r4lK5IpDB/G/wkjUu0yKGX9rbxenDI
U5PMCCjjmCXPI6T53iHTfIuJruydjsw2hUwsHXdLTcAJZSRpocQWOdKjUC/0P7GJ
BQFZG+WGYqEWCFAFgqLMBfPLZqUbMYrRKRCQCpOqR4mTjdKKUXXZhOQABGTVGiCz
sBCJZTVTgJCJD3k4vV9hHAVKzLGRRZcRdm3mLvvWz3YmJa8kqXYDjyTRQWJL5/Iq
DdSLLWzGbHKzI0PqTyZdEZJwJg8Wh/sZdvBJCf+4KFvxrjSiEpjJkFGCrKgZlnkF
QXBPHJf7uYR8+o+d3z7RqnSP6yGa
-----END CERTIFICATE-----`,
		},
	}
}
