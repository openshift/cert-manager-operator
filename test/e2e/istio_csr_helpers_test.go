//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
