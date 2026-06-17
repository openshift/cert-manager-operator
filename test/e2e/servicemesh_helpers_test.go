//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"github.com/openshift/cert-manager-operator/test/library"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	ossmIstioSystemNamespace     = "istio-system"
	ossmIstioCSRNamespace        = "istio-csr"
	ossmIstioCNINamespace        = "istio-cni"
	ossmIstioInjectionLabel      = "istio-injection"
	ossmIstioInjectionEnabled    = "enabled"
	ossmDataPlaneSelector        = "istio-injection=enabled"
	ossmDefaultIstioVersion      = "v1.24.3"
	ossmDefaultOperatorVersion   = "3.2.5"
	ossmIstiodWaitTimeout        = 15 * time.Minute
	ossmIssuerSelfSignedName     = "istio-csr-selfsigned-issuer"
	ossmRootCACertName           = "istio-csr-root-ca"
	ossmRootCASecretName         = "istio-csr-root-ca"
	ossmClusterIssuerName        = "istio-csr-cluster-issuer"
	ossmIstioSystemCACertName    = "istio-csr-ca"
	ossmIstioSystemCASecretName  = "istio-csr-ca"
	ossmIstioSystemIssuerName    = "istio-csr-issuer"
	ossmClusterExtensionName     = "servicemeshoperator3"
)

var (
	clusterExtensionGVR = schema.GroupVersionResource{
		Group:    "olm.operatorframework.io",
		Version:  "v1",
		Resource: "clusterextensions",
	}
	istioCNIGVR = schema.GroupVersionResource{
		Group:    "sailoperator.io",
		Version:  "v1",
		Resource: "istiocnis",
	}
	istioGVR = schema.GroupVersionResource{
		Group:    "sailoperator.io",
		Version:  "v1",
		Resource: "istios",
	}
)

// deriveClusterID returns the Istio multi-cluster ID derived from the API server URL.
// Example: https://api.bhb.gcp.devcluster.openshift.com:6443 -> api-bhb-gcp-devcluster-openshift-com:6443
func deriveClusterID(cfg *rest.Config) string {
	host := strings.TrimPrefix(cfg.Host, "https://")
	host = strings.TrimPrefix(host, "http://")

	hostPart, port, found := strings.Cut(host, ":")
	if !found {
		port = "6443"
	}
	return strings.ReplaceAll(hostPart, ".", "-") + ":" + port
}

// discoverIstiodControlPlaneNamespace returns the namespace of a ready istiod deployment, if any.
func discoverIstiodControlPlaneNamespace(ctx context.Context, clientset *kubernetes.Clientset) (string, bool, error) {
	deployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=istiod",
	})
	if err != nil {
		return "", false, err
	}
	for _, deployment := range deployments.Items {
		if deployment.Status.ReadyReplicas > 0 {
			return deployment.Namespace, true, nil
		}
	}

	allDeployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", false, err
	}
	for _, deployment := range allDeployments.Items {
		if deployment.Name != "istiod" && !strings.HasPrefix(deployment.Name, "istiod-") {
			continue
		}
		if deployment.Status.ReadyReplicas > 0 {
			return deployment.Namespace, true, nil
		}
	}
	return "", false, nil
}

func waitForIstiodControlPlaneNamespace(ctx context.Context, clientset *kubernetes.Clientset, timeout time.Duration) (string, error) {
	var controlPlaneNamespace string
	err := wait.PollUntilContextTimeout(ctx, fastPollInterval, timeout, true, func(context.Context) (bool, error) {
		namespace, found, err := discoverIstiodControlPlaneNamespace(ctx, clientset)
		if err != nil {
			return false, err
		}
		if !found {
			return false, nil
		}
		controlPlaneNamespace = namespace
		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("istiod control plane not available after %s: %w", timeout, err)
	}
	return controlPlaneNamespace, nil
}

func sailOperatorAPIAvailable(ctx context.Context, loader library.DynamicResourceLoader) bool {
	_, err := loader.DynamicClient.Resource(istioGVR).List(ctx, metav1.ListOptions{Limit: 1})
	return err == nil
}

func clusterExtensionAPIAvailable(ctx context.Context, loader library.DynamicResourceLoader) bool {
	_, err := loader.DynamicClient.Resource(clusterExtensionGVR).List(ctx, metav1.ListOptions{Limit: 1})
	return err == nil
}

func ossmInstallEnabled() bool {
	return os.Getenv("E2E_INSTALL_SERVICE_MESH") != "false"
}

func ossmIstioVersion() string {
	if v := os.Getenv("E2E_OSM_ISTIO_VERSION"); v != "" {
		return v
	}
	return ossmDefaultIstioVersion
}

func ossmOperatorVersion() string {
	if v := os.Getenv("E2E_OSM_OPERATOR_VERSION"); v != "" {
		return v
	}
	return ossmDefaultOperatorVersion
}

func ensureNamespace(ctx context.Context, clientset *kubernetes.Clientset, name string) error {
	_, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func applyManifestsFromFile(ctx context.Context, loader library.DynamicResourceLoader, clientset *kubernetes.Clientset, assetFn func(string) ([]byte, error), filename string) error {
	content, err := assetFn(filename)
	if err != nil {
		return fmt.Errorf("read manifest %s: %w", filename, err)
	}

	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(content), 4096)
	yamlDec := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	for {
		var raw runtime.RawExtension
		if decodeErr := decoder.Decode(&raw); decodeErr != nil {
			if decodeErr.Error() == "EOF" {
				break
			}
			return fmt.Errorf("decode manifest %s: %w", filename, decodeErr)
		}
		if len(raw.Raw) == 0 {
			continue
		}

		unstructuredObj := &unstructured.Unstructured{}
		_, gvk, err := yamlDec.Decode(raw.Raw, nil, unstructuredObj)
		if err != nil {
			return fmt.Errorf("decode object in %s: %w", filename, err)
		}

		switch gvk.GroupVersion().String() + "/" + gvk.Kind {
		case "v1/Namespace":
			if err := ensureNamespace(ctx, clientset, unstructuredObj.GetName()); err != nil {
				return err
			}
		case "v1/ServiceAccount":
			sa := &corev1.ServiceAccount{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, sa); err != nil {
				return err
			}
			_, err := clientset.CoreV1().ServiceAccounts(sa.Namespace).Create(ctx, sa, metav1.CreateOptions{})
			if err != nil && !apierrors.IsAlreadyExists(err) {
				return err
			}
		case "rbac.authorization.k8s.io/v1/ClusterRoleBinding":
			_, err := loader.DynamicClient.Resource(schema.GroupVersionResource{
				Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings",
			}).Create(ctx, unstructuredObj, metav1.CreateOptions{})
			if err != nil && !apierrors.IsAlreadyExists(err) {
				return err
			}
		default:
			gvr, gvrErr := gvrForGVK(gvk.GroupVersion().String(), gvk.Kind)
			if gvrErr != nil {
				return gvrErr
			}
			var dri dynamic.ResourceInterface
			if unstructuredObj.GetNamespace() != "" {
				dri = loader.DynamicClient.Resource(gvr).Namespace(unstructuredObj.GetNamespace())
			} else {
				dri = loader.DynamicClient.Resource(gvr)
			}
			_, err = dri.Create(ctx, unstructuredObj, metav1.CreateOptions{})
			if err != nil && !apierrors.IsAlreadyExists(err) {
				return err
			}
		}
	}
	return nil
}

func gvrForGVK(gv, kind string) (schema.GroupVersionResource, error) {
	switch gv + "/" + kind {
	case "olm.operatorframework.io/v1/ClusterExtension":
		return clusterExtensionGVR, nil
	case "sailoperator.io/v1/IstioCNI":
		return istioCNIGVR, nil
	case "sailoperator.io/v1/Istio":
		return istioGVR, nil
	default:
		return schema.GroupVersionResource{}, fmt.Errorf("unsupported GVK %s/%s", gv, kind)
	}
}

func clusterExtensionConditionTrue(obj *unstructured.Unstructured, conditionType string) bool {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, item := range conditions {
		cond, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		condStatus, _ := cond["status"].(string)
		if condType == conditionType && strings.EqualFold(condStatus, "True") {
			return true
		}
	}
	return false
}

func waitForClusterExtensionReady(ctx context.Context, loader library.DynamicResourceLoader) error {
	return wait.PollUntilContextTimeout(ctx, slowPollInterval, ossmIstiodWaitTimeout, true, func(context.Context) (bool, error) {
		ce, err := loader.DynamicClient.Resource(clusterExtensionGVR).Get(ctx, ossmClusterExtensionName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		if clusterExtensionConditionTrue(ce, "Installed") || clusterExtensionConditionTrue(ce, "Ready") {
			return true, nil
		}

		phase, found, err := unstructured.NestedString(ce.Object, "status", "phase")
		if err == nil && found && strings.EqualFold(phase, "Ready") {
			return true, nil
		}

		// Fallback: operator installed when sailoperator APIs become available.
		return sailOperatorAPIAvailable(ctx, loader), nil
	})
}

func waitForSailIstioReady(ctx context.Context, loader library.DynamicResourceLoader) error {
	return wait.PollUntilContextTimeout(ctx, slowPollInterval, ossmIstiodWaitTimeout, true, func(context.Context) (bool, error) {
		istioCR, err := loader.DynamicClient.Resource(istioGVR).Get(ctx, "default", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		status, found, err := unstructured.NestedString(istioCR.Object, "status", "state")
		if err != nil || !found {
			return false, nil
		}
		return strings.EqualFold(status, "Healthy"), nil
	})
}

func waitForSailIstioCNIReady(ctx context.Context, loader library.DynamicResourceLoader) error {
	return wait.PollUntilContextTimeout(ctx, slowPollInterval, ossmIstiodWaitTimeout, true, func(context.Context) (bool, error) {
		cniCR, err := loader.DynamicClient.Resource(istioCNIGVR).Get(ctx, "default", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if ready, found, err := unstructured.NestedBool(cniCR.Object, "status", "ready"); err == nil && found && ready {
			return true, nil
		}
		state, found, err := unstructured.NestedString(cniCR.Object, "status", "state")
		if err != nil || !found {
			return false, nil
		}
		return strings.EqualFold(state, "Healthy"), nil
	})
}

func installOSSMv3(ctx context.Context, cfg *rest.Config, loader library.DynamicResourceLoader, clientset *kubernetes.Clientset, caAddress, clusterID string) error {
	templateValues := OSSMv3Config{
		OperatorVersion: ossmOperatorVersion(),
		IstioVersion:    ossmIstioVersion(),
		ClusterID:       clusterID,
		CAAddress:       caAddress,
	}
	assetFn := AssetFunc(testassets.ReadFile).WithTemplateValues(templateValues)

	By("installing OSSM v3 ClusterExtension")
	if err := applyManifestsFromFile(ctx, loader, clientset, assetFn, filepath.Join("testdata", "servicemesh", "v3", "cluster-extension.yaml")); err != nil {
		return fmt.Errorf("apply cluster extension: %w", err)
	}
	Expect(waitForClusterExtensionReady(ctx, loader)).NotTo(HaveOccurred(), "ClusterExtension should become ready before installing Istio operands")

	By("installing OSSM v3 IstioCNI")
	if err := applyManifestsFromFile(ctx, loader, clientset, assetFn, filepath.Join("testdata", "servicemesh", "v3", "istio-cni.yaml")); err != nil {
		return fmt.Errorf("apply istio cni: %w", err)
	}
	Expect(waitForSailIstioCNIReady(ctx, loader)).NotTo(HaveOccurred(), "IstioCNI should become ready")

	By("installing OSSM v3 Istio control plane wired to istio-csr")
	if err := applyManifestsFromFile(ctx, loader, clientset, assetFn, filepath.Join("testdata", "servicemesh", "v3", "istio.yaml")); err != nil {
		return fmt.Errorf("apply istio: %w", err)
	}
	Expect(waitForSailIstioReady(ctx, loader)).NotTo(HaveOccurred(), "Istio CR should become Healthy")

	_, err := waitForIstiodControlPlaneNamespace(ctx, clientset, ossmIstiodWaitTimeout)
	return err
}

func ensureOSSMIssuerChain(ctx context.Context, clientset *kubernetes.Clientset, certClient *certmanagerclientset.Clientset) error {
	By("creating self-signed issuer for istio-csr root CA in cert-manager namespace")
	selfSignedIssuer := &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ossmIssuerSelfSignedName,
			Namespace: operandNamespace,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}
	_, err := certClient.CertmanagerV1().Issuers(operandNamespace).Create(ctx, selfSignedIssuer, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	By("creating istio-csr root CA certificate in cert-manager namespace")
	rootCA := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ossmRootCACertName,
			Namespace: operandNamespace,
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: ossmRootCACertName,
			SecretName: ossmRootCASecretName,
			IsCA:       true,
			Duration:   &metav1.Duration{Duration: 3 * time.Hour},
			IssuerRef: certmanagermetav1.ObjectReference{
				Name:  ossmIssuerSelfSignedName,
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
		},
	}
	_, err = certClient.CertmanagerV1().Certificates(operandNamespace).Create(ctx, rootCA, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err := waitForCertificateReadiness(ctx, ossmRootCACertName, operandNamespace); err != nil {
		return err
	}

	By("creating istio-csr cluster issuer")
	clusterIssuer := &certmanagerv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{Name: ossmClusterIssuerName},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{
					SecretName: ossmRootCASecretName,
				},
			},
		},
	}
	_, err = certClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err := waitForClusterIssuerReadiness(ctx, ossmClusterIssuerName); err != nil {
		return err
	}

	if err := ensureNamespace(ctx, clientset, ossmIstioSystemNamespace); err != nil {
		return err
	}

	By("creating istio-system CA certificate")
	istioCA := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ossmIstioSystemCACertName,
			Namespace: ossmIstioSystemNamespace,
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: ossmIstioSystemCACertName,
			SecretName: ossmIstioSystemCASecretName,
			IsCA:       true,
			Duration:   &metav1.Duration{Duration: 2 * time.Hour},
			IssuerRef: certmanagermetav1.ObjectReference{
				Name:  ossmClusterIssuerName,
				Kind:  "ClusterIssuer",
				Group: "cert-manager.io",
			},
		},
	}
	_, err = certClient.CertmanagerV1().Certificates(ossmIstioSystemNamespace).Create(ctx, istioCA, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err := waitForCertificateReadiness(ctx, ossmIstioSystemCACertName, ossmIstioSystemNamespace); err != nil {
		return err
	}

	By("creating istio-system issuer for istio-csr operand")
	istioIssuer := &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ossmIstioSystemIssuerName,
			Namespace: ossmIstioSystemNamespace,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{
					SecretName: ossmIstioSystemCASecretName,
				},
			},
		},
	}
	_, err = certClient.CertmanagerV1().Issuers(ossmIstioSystemNamespace).Create(ctx, istioIssuer, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return waitForIssuerReadiness(ctx, ossmIstioSystemIssuerName, ossmIstioSystemNamespace)
}

func ensureOSSMIstioCSROperand(ctx context.Context, loader library.DynamicResourceLoader, clusterID string) error {
	clientset, ok := loader.KubeClient.(*kubernetes.Clientset)
	if !ok {
		return fmt.Errorf("kubernetes client is not a *kubernetes.Clientset")
	}
	if err := ensureNamespace(ctx, clientset, ossmIstioCSRNamespace); err != nil {
		return err
	}

	By("creating IstioCSR operand for OSSM v3 smoke")
	loader.CreateFromFile(
		AssetFunc(testassets.ReadFile).WithTemplateValues(OSSMIstioCSROperandConfig{
			Namespace: ossmIstioCSRNamespace,
			ClusterID: clusterID,
		}),
		filepath.Join("testdata", "servicemesh", "istio-csr-operand.yaml"),
		ossmIstioCSRNamespace,
	)
	return nil
}

// ensureServiceMeshForSmoke discovers or installs OSSM v3 and returns the istiod namespace.
func ensureServiceMeshForSmoke(ctx context.Context, cfg *rest.Config, loader library.DynamicResourceLoader, clientset *kubernetes.Clientset, caAddress, clusterID string) (string, error) {
	if namespace, found, err := discoverIstiodControlPlaneNamespace(ctx, clientset); err != nil {
		return "", err
	} else if found {
		By(fmt.Sprintf("reusing existing istiod control plane in namespace %s", namespace))
		if sailOperatorAPIAvailable(ctx, loader) {
			_, getErr := loader.DynamicClient.Resource(istioGVR).Get(ctx, "default", metav1.GetOptions{})
			if getErr == nil {
				if err := waitForSailIstioReady(ctx, loader); err != nil {
					return "", fmt.Errorf("existing Istio CR is not Healthy: %w", err)
				}
			} else if !apierrors.IsNotFound(getErr) {
				return "", getErr
			}
		}
		if _, err := waitForIstiodControlPlaneNamespace(ctx, clientset, lowTimeout); err != nil {
			return "", fmt.Errorf("existing istiod is not ready: %w", err)
		}
		return namespace, nil
	}

	if !ossmInstallEnabled() {
		return "", fmt.Errorf("istiod control plane not found and E2E_INSTALL_SERVICE_MESH=false")
	}
	if !clusterExtensionAPIAvailable(ctx, loader) {
		return "", fmt.Errorf("ClusterExtension API (olm.operatorframework.io/v1) is not available on this cluster")
	}

	By("installing OpenShift Service Mesh v3 for multi-operand smoke tests")
	if err := installOSSMv3(ctx, cfg, loader, clientset, caAddress, clusterID); err != nil {
		return "", err
	}
	return ossmIstioSystemNamespace, nil
}

func labelNamespaceForIstioInjection(ctx context.Context, clientset *kubernetes.Clientset, namespace string) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		ns, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		ns.Labels[ossmIstioInjectionLabel] = ossmIstioInjectionEnabled
		_, err = clientset.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
}

func deployMeshSampleWorkloads(ctx context.Context, loader library.DynamicResourceLoader, namespace string) {
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "servicemesh", "httpbin.yaml"), namespace)
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "servicemesh", "sleep.yaml"), namespace)
}

func waitForInjectedDeploymentReady(ctx context.Context, clientset *kubernetes.Clientset, namespace, deploymentName string) error {
	return wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if deployment.Status.ReadyReplicas < 1 {
			return false, nil
		}

		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app=%s", deploymentName),
		})
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			if len(pod.Spec.Containers) < 2 {
				return false, nil
			}
			appReady := false
			sidecarReady := false
			for _, cs := range pod.Status.ContainerStatuses {
				if !cs.Ready {
					continue
				}
				if cs.Name == "istio-proxy" {
					sidecarReady = true
					continue
				}
				appReady = true
			}
			if appReady && sidecarReady {
				return true, nil
			}
		}
		return false, nil
	})
}

func waitForCertificateRequestsFromIstioCSR(ctx context.Context, namespace string, minCount int) error {
	return wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		crs, err := certmanagerClient.CertmanagerV1().CertificateRequests(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		count := 0
		for _, cr := range crs.Items {
			if strings.HasPrefix(cr.Name, "istio-csr-") {
				count++
			}
		}
		return count >= minCount, nil
	})
}

func getRunningPodName(ctx context.Context, clientset *kubernetes.Clientset, namespace, labelSelector string) (string, error) {
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return "", err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Name, nil
		}
	}
	return "", fmt.Errorf("no running pod found for selector %s in namespace %s", labelSelector, namespace)
}

