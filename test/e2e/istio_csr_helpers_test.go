//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type LogEntry struct {
	CertChain []string `json:"certChain"`
}

const grpcurlLogExcerptMaxLen = 512

func formatGRPCurlLogExcerpt(logData []byte) string {
	if len(logData) == 0 {
		return "<empty>"
	}

	excerpt := logData
	if len(excerpt) > grpcurlLogExcerptMaxLen {
		excerpt = append(append([]byte(nil), excerpt[:grpcurlLogExcerptMaxLen]...), []byte("...")...)
	}
	return strings.ReplaceAll(string(excerpt), "\n", `\n`)
}

// parseGRPCurlLogEntry extracts the grpcurl CreateCertificate JSON response from pod logs.
// It accepts compact or multi-line JSON, and falls back to the last valid line when retries
// leave non-JSON noise before or after the response.
func parseGRPCurlLogEntry(logData []byte) (LogEntry, error) {
	trimmed := bytes.TrimSpace(logData)
	var entry LogEntry
	if len(trimmed) > 0 && json.Unmarshal(trimmed, &entry) == nil {
		return entry, nil
	}

	lines := bytes.Split(trimmed, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		if err := json.Unmarshal(line, &entry); err == nil {
			return entry, nil
		}
	}

	return LogEntry{}, fmt.Errorf(
		"no valid grpcurl JSON log entry found in pod logs (excerpt: %s)",
		formatGRPCurlLogExcerpt(trimmed),
	)
}

// waitForIstioCSROperandReady waits for the cert-manager-istio-csr deployment and IstioCSR CR status.
func waitForIstioCSROperandReady(ctx context.Context, clientset *kubernetes.Clientset, loader library.DynamicResourceLoader, namespace string) (v1alpha1.IstioCSRStatus, error) {
	if err := pollTillDeploymentAvailable(ctx, clientset, namespace, istioCSRGRPCServiceName); err != nil {
		return v1alpha1.IstioCSRStatus{}, err
	}
	return pollTillIstioCSRAvailable(ctx, loader, namespace, istioCSRResourceName)
}

// expectIstioCSROperandReady waits for the operand to become ready and fails the spec on error.
func expectIstioCSROperandReady(ctx context.Context, clientset *kubernetes.Clientset, loader library.DynamicResourceLoader, namespace string) v1alpha1.IstioCSRStatus {
	By("waiting for IstioCSR operand to become ready")
	status, err := waitForIstioCSROperandReady(ctx, clientset, loader, namespace)
	Expect(err).NotTo(HaveOccurred())
	return status
}

// restartIstioCSRDeployment rolls out cert-manager-istio-csr so it picks up current TLS material.
// OSSM smoke tests delete the IstioCSR CR between runs without removing the deployment, which can
// leave a stale gRPC serving certificate that no longer matches istiod-tls.
func restartIstioCSRDeployment(ctx context.Context, clientset *kubernetes.Clientset, namespace string) error {
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, istioCSRGRPCServiceName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get deployment %s/%s: %w", namespace, istioCSRGRPCServiceName, err)
	}

	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["cert-manager-operator-e2e/restartedAt"] = time.Now().Format(time.RFC3339)

	if _, err := clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("restart deployment %s/%s: %w", namespace, istioCSRGRPCServiceName, err)
	}
	return pollTillDeploymentAvailable(ctx, clientset, namespace, istioCSRGRPCServiceName)
}

// findIstiodDeploymentInNamespace locates the istiod deployment (standard or revisioned like istiod-*) in the given namespace.
func findIstiodDeploymentInNamespace(ctx context.Context, clientset *kubernetes.Clientset, namespace string) (*appsv1.Deployment, error) {
	deployments, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=istiod",
	})
	if err == nil && len(deployments.Items) > 0 {
		return &deployments.Items[0], nil
	}

	allDeployments, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments in %s: %w", namespace, err)
	}
	for _, deployment := range allDeployments.Items {
		if deployment.Name == "istiod" || strings.HasPrefix(deployment.Name, "istiod-") {
			return &deployment, nil
		}
	}
	return nil, fmt.Errorf("no istiod deployment found in namespace %s", namespace)
}

// restartIstiodDeployment bounces the istiod Deployment in the given control-plane
// namespace so it immediately re-dials the (possibly restarted) istio-csr gRPC
// endpoint and re-requests its istiod-tls certificate. Without this, an existing
// istiod process may take >10 minutes to organically reconnect after istio-csr is
// restarted, causing waitForIstiodTLSIssuerCAAligned to time out.
func restartIstiodDeployment(ctx context.Context, clientset *kubernetes.Clientset, namespace string) error {
	deployment, err := findIstiodDeploymentInNamespace(ctx, clientset, namespace)
	if err != nil {
		return err
	}

	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["cert-manager-operator-e2e/restartedAt"] = time.Now().Format(time.RFC3339)

	if _, err := clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("restart deployment %s/%s: %w", namespace, deployment.Name, err)
	}
	return pollTillDeploymentAvailable(ctx, clientset, namespace, deployment.Name)
}
