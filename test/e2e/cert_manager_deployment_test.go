//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"embed"
	_ "embed"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	opv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	"github.com/openshift/cert-manager-operator/test/library"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"github.com/stretchr/testify/require"
)

const (
	PollInterval = time.Second
	TestTimeout  = 10 * time.Minute
)

//go:embed testdata/*
var testassets embed.FS

func TestSelfSignedCerts(t *testing.T) {
	ctx := context.Background()
	loader := library.NewDynamicResourceLoader(ctx, t)

	loader.CreateTestingNS("hello")
	defer loader.DeleteTestingNS("hello")
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"))
	defer loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"))

	err := wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
		// TODO: The loader.KubeClient might be worth splitting out. Let's see once we have more tests.
		secret, err := loader.KubeClient.CoreV1().Secrets("hello").Get(ctx, "root-secret", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			t.Logf("Unable to retrieve the root secret: %v", err)
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return library.VerifySecretNotNull(secret)
	})
	require.NoError(t, err)
}

func TestACMECertsIngress(t *testing.T) {
	ctx := context.Background()
	loader := library.NewDynamicResourceLoader(ctx, t)
	config, err := library.GetConfigForTest(t)
	require.NoError(t, err)

	loader.CreateTestingNS("hello")
	defer loader.DeleteTestingNS("hello")
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "clusterissuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "clusterissuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"))

	routeV1Client, err := routev1.NewForConfig(config)
	require.NoError(t, err)
	route, err := routeV1Client.Routes("openshift-console").Get(ctx, "console", metav1.GetOptions{})
	require.NoError(t, err)

	ingress_host := "hey." + strings.Join(strings.Split(route.Spec.Host, ".")[1:], ".")
	path_type := networkingv1.PathTypePrefix
	ingress := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ingress-le-prod",
			Namespace: "hello",
			Annotations: map[string]string{
				"cert-manager.io/cluster-issuer":            "letsencrypt-prod",
				"acme.cert-manager.io/http01-ingress-class": "openshift-default",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: ingress_host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &path_type,
									Backend:  networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "hello-openshift", Port: networkingv1.ServiceBackendPort{Number: 8080}}},
								},
							},
						},
					},
				},
			},
			TLS: []networkingv1.IngressTLS{{
				Hosts:      []string{ingress_host},
				SecretName: "ingress-prod-secret",
			}},
		},
	}
	ingress, err = loader.KubeClient.NetworkingV1().Ingresses(ingress.ObjectMeta.Namespace).Create(ctx, ingress, metav1.CreateOptions{})
	require.NoError(t, err)
	defer loader.KubeClient.NetworkingV1().Ingresses(ingress.ObjectMeta.Namespace).Delete(ctx, ingress.ObjectMeta.Name, metav1.DeleteOptions{})

	err = wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
		secret, err := loader.KubeClient.CoreV1().Secrets(ingress.ObjectMeta.Namespace).Get(ctx, "ingress-prod-secret", metav1.GetOptions{})
		tlsConfig, isvalid := library.GetTLSConfig(secret)
		if !isvalid {
			t.Logf("Unable to retrieve the TLS config: %v", err)
			return false, nil
		}
		is_host_correct, err := library.VerifyHostname(ingress_host, tlsConfig.Clone())
		if err != nil {
			t.Logf("Host: %v", err)
			return false, nil
		}
		is_not_expired, err := library.VerifyExpiry(ingress_host+":443", tlsConfig.Clone())
		if err != nil {
			t.Logf("Expired: %v", err)
			return false, nil
		}
		return is_host_correct && is_not_expired, nil
	})
	require.NoError(t, err)
}

func TestCertRenew(t *testing.T) {
	ctx := context.Background()
	loader := library.NewDynamicResourceLoader(ctx, t)
	config, err := library.GetConfigForTest(t)
	require.NoError(t, err)

	loader.CreateTestingNS("hello")
	defer loader.DeleteTestingNS("hello")
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"))
	defer loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"))

	routeV1Client, err := routev1.NewForConfig(config)
	require.NoError(t, err)
	route, err := routeV1Client.Routes("openshift-console").Get(ctx, "console", metav1.GetOptions{})
	require.NoError(t, err)

	ingress_host := "hello-test." + strings.Join(strings.Split(route.Spec.Host, ".")[1:], ".")
	path_type := networkingv1.PathTypePrefix
	ingress := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "frontend",
			Namespace: "hello",
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: ingress_host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &path_type,
									Backend:  networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "hello-openshift", Port: networkingv1.ServiceBackendPort{Number: 8080}}},
								},
							},
						},
					},
				},
			},
			TLS: []networkingv1.IngressTLS{{
				Hosts:      []string{ingress_host},
				SecretName: "selfsigned-server-cert-tls",
			}},
		},
	}
	ingress, err = loader.KubeClient.NetworkingV1().Ingresses(ingress.ObjectMeta.Namespace).Create(ctx, ingress, metav1.CreateOptions{})
	require.NoError(t, err)
	defer loader.KubeClient.NetworkingV1().Ingresses(ingress.ObjectMeta.Namespace).Delete(ctx, ingress.ObjectMeta.Name, metav1.DeleteOptions{})

	crt := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "selfsigned-server-cert",
			Namespace: "hello",
		},
		Spec: certmanagerv1.CertificateSpec{
			DNSNames:    []string{ingress_host, "server"},
			SecretName:  "selfsigned-server-cert-tls",
			IsCA:        false,
			Duration:    &metav1.Duration{Duration: time.Hour},
			RenewBefore: &metav1.Duration{Duration: time.Minute * 59},
			Usages:      []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth},
			IssuerRef: certmanagermetav1.ObjectReference{
				Name:  "my-ca-issuer",
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
		},
	}
	certmanagerv1Client, err := certmanagerclientset.NewForConfig(config)
	require.NoError(t, err)
	crt, err = certmanagerv1Client.CertmanagerV1().Certificates(crt.ObjectMeta.Namespace).Create(ctx, crt, metav1.CreateOptions{})
	defer certmanagerv1Client.CertmanagerV1().Certificates(crt.ObjectMeta.Namespace).Delete(ctx, crt.ObjectMeta.Name, metav1.DeleteOptions{})
	require.NoError(t, err)
	err = wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
		secret, _ := loader.KubeClient.CoreV1().Secrets("hello").Get(ctx, crt.Spec.SecretName, metav1.GetOptions{})
		tlsConfig, isValid := library.GetTLSConfig(secret)
		if !isValid {
			return false, nil
		}

		is_host_correct, err := library.VerifyHostname(ingress_host, tlsConfig.Clone())
		if err != nil {
			t.Errorf("Host %v", err)
			return false, nil
		}
		is_not_expired, err := library.VerifyExpiry(ingress_host+":443", tlsConfig.Clone())
		if err != nil {
			t.Errorf("Expiry %v", err)
			return false, nil
		}
		expiryTime, err := library.GetCertExpiry(ingress_host+":443", tlsConfig.Clone())
		t.Logf("Expiry Before %v", expiryTime)
		if err != nil {
			return false, nil
		}
		time.Sleep(time.Minute + time.Second*5)
		secret, _ = loader.KubeClient.CoreV1().Secrets("hello").Get(ctx, crt.Spec.SecretName, metav1.GetOptions{})
		tlsConfig, isValid = library.GetTLSConfig(secret)
		if !isValid {
			return false, nil
		}
		expiryTimeNew, err := library.GetCertExpiry(ingress_host+":443", tlsConfig.Clone())
		t.Logf("Expiry After %v", expiryTimeNew)
		if err != nil {
			return false, nil
		}
		is_cert_renewed := expiryTimeNew.After(expiryTime)
		return is_host_correct && is_not_expired && is_cert_renewed, nil
	})
	require.NoError(t, err)
}

func TestContainerOverrides(t *testing.T) {
	ctx := context.Background()
	config, err := library.GetConfigForTest(t)
	require.NoError(t, err)

	certmanageroperatorclient, err := certmanoperatorclient.NewForConfig(config)
	require.NoError(t, err)

	operator, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	require.NoError(t, err)
	defer func() {
		err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Delete(ctx, "cluster", metav1.DeleteOptions{})
		require.NoError(t, err)
	}()

	verifyValidControllerOperatorStatus(t, certmanageroperatorclient)
	updatedOperator := operator.DeepCopy()

	addValidControlleDeploymentConfig(updatedOperator)
	_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, updatedOperator, metav1.UpdateOptions{})
	require.NoError(t, err)

	verifyValidControllerOperatorStatus(t, certmanageroperatorclient)

	err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Delete(ctx, "cluster", metav1.DeleteOptions{})
	require.NoError(t, err)

	verifyValidControllerOperatorStatus(t, certmanageroperatorclient)

	operator, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	require.NoError(t, err)

	updatedOperator = operator.DeepCopy()
	addInvalidControlleOverrideArgs(updatedOperator)
	_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, updatedOperator, metav1.UpdateOptions{})
	require.NoError(t, err)

	verifyInvalidControllerOperatorStatus(t, certmanageroperatorclient)

	operator, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	require.NoError(t, err)

	updatedOperator = operator.DeepCopy()
	addInvalidControlleOverrideEnv(updatedOperator)
	_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, updatedOperator, metav1.UpdateOptions{})
	require.NoError(t, err)

	verifyInvalidControllerOperatorStatus(t, certmanageroperatorclient)

	// reset operator spec
	operator, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	require.NoError(t, err)

	updatedOperator = operator.DeepCopy()
	updatedOperator.Spec.ControllerConfig = nil
	_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, updatedOperator, metav1.UpdateOptions{})
	require.NoError(t, err)
}

func verifyValidControllerOperatorStatus(t *testing.T, client *certmanoperatorclient.Clientset) {

	t.Log("verifying valid controller operator status")
	err := wait.PollImmediate(time.Second*5, time.Minute*5, func() (done bool, err error) {
		operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if operator.DeletionTimestamp != nil {
			return false, nil
		}

		flag := false
		for _, cond := range operator.Status.Conditions {
			if cond.Type == "cert-manager-controller-deploymentAvailable" {
				flag = cond.Status == opv1.ConditionTrue
			}

			if cond.Type == "cert-manager-controller-deploymentDegraded" {
				flag = cond.Status == opv1.ConditionFalse
			}

			if cond.Type == "cert-manager-controller-deploymentProgressing" {
				flag = cond.Status == opv1.ConditionFalse
			}
		}

		t.Logf("Current poll status: %v", flag)
		return flag, nil
	})
	require.NoError(t, err)
}

func addValidControlleDeploymentConfig(operator *v1alpha1.CertManager) {
	operator.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{
		OverrideArgs: []string{"--dns01-recursive-nameservers=10.10.10.10:53", "--dns01-recursive-nameservers-only"},
		OverrideEnv: []corev1.EnvVar{
			{
				Name:  "HTTP_PROXY",
				Value: "172.0.0.10:8080",
			},
		},
	}
}

func addInvalidControlleOverrideArgs(operator *v1alpha1.CertManager) {
	operator.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{
		OverrideArgs: []string{"--invalid-args=foo"},
	}
}

func addInvalidControlleOverrideEnv(operator *v1alpha1.CertManager) {
	operator.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{
		OverrideEnv: []corev1.EnvVar{
			{
				Name:  "FOO",
				Value: "BAR",
			},
		},
	}
}

func verifyInvalidControllerOperatorStatus(t *testing.T, client *certmanoperatorclient.Clientset) {
	t.Log("verifying invalid controller operator status")
	err := wait.PollImmediate(time.Second*5, time.Minute*5, func() (done bool, err error) {

		operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if operator.DeletionTimestamp != nil {
			return false, nil
		}

		flag := false
		for _, cond := range operator.Status.Conditions {
			if cond.Type == "cert-manager-controller-deploymentDegraded" {
				flag = cond.Status == opv1.ConditionTrue
			}
		}

		t.Logf("Current poll status: %v", flag)
		return flag, nil
	})
	require.NoError(t, err)

}
