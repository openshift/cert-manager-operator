package e2e

import (
	"context"
	"embed"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	apimanifests "github.com/operator-framework/api/pkg/manifests"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/openshift/cert-manager-operator/test/library"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	scapiv1alpha3 "github.com/operator-framework/api/pkg/apis/scorecard/v1alpha3"

	// "github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	PollInterval = time.Second
	TestTimeout  = 10 * time.Minute
)

//go:embed testdata/*
var testassets embed.FS

// create type Tests
type Tests struct {
	T *testing.T
}

func (test Tests) TestSelfSignedCerts(bundle *apimanifests.Bundle) scapiv1alpha3.TestResult {
	ctx := context.Background()
	t := test.T
	loader := library.NewDynamicResourceLoader(ctx, t)

	r := scapiv1alpha3.TestResult{}
	r.Name = "TestSelfSignedCerts"
	r.State = scapiv1alpha3.PassState
	r.Errors = []string{}
	r.Suggestions = []string{}

	loader.CreateTestingNS("hello")
	defer loader.DeleteTestingNS("hello")
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"))
	defer loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"))

	err := wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
		secret, err := loader.KubeClient.CoreV1().Secrets("hello").Get(ctx, "root-secret", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			log.Print(err)
			return false, nil
		} else if err != nil {
			return false, err
		}
		return library.VerifySecretNotNull(secret)
	})
	log.Print("Certificate generated root-secret found")
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}
	return r
}

func (test Tests) TestACMECertsIngress(bundle *apimanifests.Bundle) scapiv1alpha3.TestResult {
	ctx := context.Background()
	t := &testing.T{}
	loader := library.NewDynamicResourceLoader(ctx, t)
	config, err := library.GetConfigForTest(t)
	r := scapiv1alpha3.TestResult{}
	r.Name = "TestACMECertsIngress"
	r.State = scapiv1alpha3.PassState
	r.Errors = make([]string, 0)
	r.Suggestions = make([]string, 0)
	if err != nil {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
		return r
	}

	loader.CreateTestingNS("hello")
	defer loader.DeleteTestingNS("hello")
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "clusterissuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "clusterissuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"))

	routeV1Client, err := routev1.NewForConfig(config)
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}
	route, err := routeV1Client.Routes("openshift-console").Get(ctx, "console", metav1.GetOptions{})
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}

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
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}
	defer loader.KubeClient.NetworkingV1().Ingresses(ingress.ObjectMeta.Namespace).Delete(ctx, ingress.ObjectMeta.Name, metav1.DeleteOptions{})

	err = wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
		secret, err := loader.KubeClient.CoreV1().Secrets(ingress.ObjectMeta.Namespace).Get(ctx, "ingress-prod-secret", metav1.GetOptions{})
		tlsConfig, isvalid := library.GetTLSConfig(secret)
		if !isvalid {
			log.Printf("Polling for the TLS config: %v", err)
			return false, nil
		}
		is_host_correct, err := library.VerifyHostname(ingress_host, tlsConfig.Clone())
		if err != nil {
			log.Printf("Polling until the host is correct: %v", err)
			return false, nil
		}
		is_not_expired, err := library.VerifyExpiry(ingress_host+":443", tlsConfig.Clone())
		if err != nil {
			log.Printf("Polling until the expiry is correct: %v", err)
			return false, nil
		}
		log.Printf("Hostname is correct: %v", is_host_correct)
		log.Printf("Certificate is not expired: %v", is_not_expired)
		return is_host_correct && is_not_expired, err
	})
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}
	return r
}

func (test Tests) TestCertRenewal(bundle *apimanifests.Bundle) scapiv1alpha3.TestResult {
	ctx := context.Background()
	t := &testing.T{}
	loader := library.NewDynamicResourceLoader(ctx, t)
	r := scapiv1alpha3.TestResult{}
	r.Name = "TestCertRenewal"
	r.State = scapiv1alpha3.PassState
	r.Errors = make([]string, 0)
	r.Suggestions = make([]string, 0)
	config, err := library.GetConfigForTest(t)
	if !assert.NoError(t, err) {
		r.State = scapiv1alpha3.FailState
		r.Errors = append(r.Errors, err.Error())
		return r
	}
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
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}
	route, err := routeV1Client.Routes("openshift-console").Get(ctx, "console", metav1.GetOptions{})
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}

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
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}
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
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}
	crt, err = certmanagerv1Client.CertmanagerV1().Certificates(crt.ObjectMeta.Namespace).Create(ctx, crt, metav1.CreateOptions{})
	defer certmanagerv1Client.CertmanagerV1().Certificates(crt.ObjectMeta.Namespace).Delete(ctx, crt.ObjectMeta.Name, metav1.DeleteOptions{})
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}
	err = wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
		secret, _ := loader.KubeClient.CoreV1().Secrets("hello").Get(ctx, crt.Spec.SecretName, metav1.GetOptions{})
		tlsConfig, isValid := library.GetTLSConfig(secret)
		if !isValid {
			return false, nil
		}

		is_host_correct, err := library.VerifyHostname(ingress_host, tlsConfig.Clone())
		if err != nil {
			t.Errorf("Host is incorrect %v", err)
			return false, nil
		}
		is_not_expired, err := library.VerifyExpiry(ingress_host+":443", tlsConfig.Clone())
		if err != nil {
			t.Errorf("Certificate is expired %v", err)
			return false, nil
		}
		expiryTime, err := library.GetCertExpiry(ingress_host+":443", tlsConfig.Clone())
		log.Printf("Expiry time before renew %v", expiryTime)
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
		is_cert_renewed := expiryTimeNew.After(expiryTime)
		if is_cert_renewed {
			log.Printf("Expiry time after renew %v", expiryTimeNew)
		}
		if err != nil {
			return false, nil
		}
		log.Printf("Hostname is correct: %v", is_host_correct)
		log.Printf("Certificate is not expired: %v", is_not_expired)
		log.Printf("Certificate is renewed: %v", is_cert_renewed)
		return is_host_correct && is_not_expired && is_cert_renewed, nil
	})
	if !assert.NoError(t, err) {
		r.Errors = append(r.Errors, err.Error())
		r.State = scapiv1alpha3.FailState
	}
	return r
}
