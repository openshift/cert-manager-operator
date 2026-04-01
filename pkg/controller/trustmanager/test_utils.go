package trustmanager

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr/testr"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/testutil"
)

var errTestClient = fmt.Errorf("test client error")

type trustManagerBuilder struct {
	*v1alpha1.TrustManager
}

func testTrustManager() *trustManagerBuilder {
	return &trustManagerBuilder{
		TrustManager: &v1alpha1.TrustManager{
			ObjectMeta: metav1.ObjectMeta{
				Name: trustManagerObjectName,
			},
			Spec: v1alpha1.TrustManagerSpec{
				TrustManagerConfig: v1alpha1.TrustManagerConfig{
					LogLevel:       1,
					LogFormat:      "text",
					TrustNamespace: defaultTrustNamespace,
				},
			},
		},
	}
}

func (b *trustManagerBuilder) WithLabels(labels map[string]string) *trustManagerBuilder {
	b.Spec.ControllerConfig.Labels = labels
	return b
}

func (b *trustManagerBuilder) WithAnnotations(annotations map[string]string) *trustManagerBuilder {
	b.Spec.ControllerConfig.Annotations = annotations
	return b
}

func (b *trustManagerBuilder) WithResources(resources corev1.ResourceRequirements) *trustManagerBuilder {
	b.Spec.TrustManagerConfig.Resources = resources
	return b
}

func (b *trustManagerBuilder) WithTolerations(tolerations []corev1.Toleration) *trustManagerBuilder {
	b.Spec.TrustManagerConfig.Tolerations = tolerations
	return b
}

func (b *trustManagerBuilder) WithNodeSelector(nodeSelector map[string]string) *trustManagerBuilder {
	b.Spec.TrustManagerConfig.NodeSelector = nodeSelector
	return b
}

func (b *trustManagerBuilder) WithAffinity(affinity *corev1.Affinity) *trustManagerBuilder {
	b.Spec.TrustManagerConfig.Affinity = affinity
	return b
}

func (b *trustManagerBuilder) WithLogLevel(level int32) *trustManagerBuilder {
	b.Spec.TrustManagerConfig.LogLevel = level
	return b
}

func (b *trustManagerBuilder) WithLogFormat(format string) *trustManagerBuilder {
	b.Spec.TrustManagerConfig.LogFormat = format
	return b
}

func (b *trustManagerBuilder) WithTrustNamespace(ns string) *trustManagerBuilder {
	b.Spec.TrustManagerConfig.TrustNamespace = ns
	return b
}

func (b *trustManagerBuilder) WithFilterExpiredCertificates(policy v1alpha1.FilterExpiredCertificatesPolicy) *trustManagerBuilder {
	b.Spec.TrustManagerConfig.FilterExpiredCertificates = policy
	return b
}

func (b *trustManagerBuilder) WithSecretTargets(policy v1alpha1.SecretTargetsPolicy, authorizedSecrets []string) *trustManagerBuilder {
	b.Spec.TrustManagerConfig.SecretTargets = v1alpha1.SecretTargetsConfig{
		Policy:            policy,
		AuthorizedSecrets: authorizedSecrets,
	}
	return b
}

func (b *trustManagerBuilder) Build() *v1alpha1.TrustManager {
	return b.TrustManager
}

func testReconciler(t *testing.T) *Reconciler {
	return &Reconciler{
		ctx:           context.Background(),
		eventRecorder: record.NewFakeRecorder(100),
		log:           testr.New(t),
		scheme:        testutil.Scheme,
	}
}

func testResourceLabels() map[string]string {
	return getResourceLabels(testTrustManager().Build())
}

func testResourceAnnotations() map[string]string {
	return getResourceAnnotations(testTrustManager().Build())
}

func assertError(t *testing.T, err error, wantErr string) {
	t.Helper()
	if wantErr != "" {
		if err == nil {
			t.Errorf("expected error containing %q, got nil", wantErr)
			return
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Errorf("expected error containing %q, got %q", wantErr, err.Error())
		}
	} else if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
