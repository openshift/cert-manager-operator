//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"
)

type LogEntry struct {
	CertChain []string `json:"certChain"`
}

// parseGRPCurlLogEntry returns the last valid JSON log line from a grpcurl job pod.
// Pod logs may contain noise from retries or incomplete trailing lines.
func parseGRPCurlLogEntry(logData []byte) (LogEntry, error) {
	lines := bytes.Split(bytes.TrimSpace(logData), []byte("\n"))
	var entry LogEntry
	for i := len(lines) - 1; i >= 0; i-- {
		if len(lines[i]) == 0 {
			continue
		}
		if err := json.Unmarshal(lines[i], &entry); err == nil {
			return entry, nil
		}
	}
	return LogEntry{}, fmt.Errorf("no valid grpcurl JSON log entry found in pod logs")
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
