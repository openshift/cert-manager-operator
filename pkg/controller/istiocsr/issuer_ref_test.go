package istiocsr

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

func TestResolvedIssuerRefDefaults(t *testing.T) {
	ref := resolvedIssuerRef(certmanagermetav1.IssuerReference{Name: "example"})
	if ref.Kind != defaultIssuerKind {
		t.Fatalf("expected kind %q, got %q", defaultIssuerKind, ref.Kind)
	}
	if ref.Group != defaultIssuerGroup {
		t.Fatalf("expected group %q, got %q", defaultIssuerGroup, ref.Group)
	}
}

func TestIsCertManagerBuiltInIssuer(t *testing.T) {
	if !isCertManagerBuiltInIssuer(certmanagermetav1.IssuerReference{Name: "x", Kind: "Issuer", Group: "cert-manager.io"}) {
		t.Fatal("expected built-in Issuer to match")
	}
	if isCertManagerBuiltInIssuer(certmanagermetav1.IssuerReference{Name: "x", Kind: "GoogleCASIssuer", Group: "cas-issuer.jetstack.io"}) {
		t.Fatal("expected external issuer not to match built-in check")
	}
}

func TestIsAcceptedIssuerReference(t *testing.T) {
	tests := []struct {
		name string
		ref  certmanagermetav1.IssuerReference
		want bool
	}{
		{
			name: "built-in issuer",
			ref:  certmanagermetav1.IssuerReference{Name: "x", Kind: "Issuer", Group: "cert-manager.io"},
			want: true,
		},
		{
			name: "built-in cluster issuer",
			ref:  certmanagermetav1.IssuerReference{Name: "x", Kind: "ClusterIssuer", Group: "cert-manager.io"},
			want: true,
		},
		{
			name: "external issuer kind",
			ref:  certmanagermetav1.IssuerReference{Name: "x", Kind: "GoogleCASIssuer", Group: "cas-issuer.jetstack.io"},
			want: true,
		},
		{
			name: "default kind and group",
			ref:  certmanagermetav1.IssuerReference{Name: "x"},
			want: true,
		},
		{
			name: "deployment is not an issuer",
			ref:  certmanagermetav1.IssuerReference{Name: "x", Kind: "Deployment", Group: "apps"},
			want: false,
		},
		{
			name: "cluster role is not an issuer",
			ref:  certmanagermetav1.IssuerReference{Name: "x", Kind: "ClusterRole", Group: "rbac.authorization.k8s.io"},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAcceptedIssuerReference(tt.ref); got != tt.want {
				t.Fatalf("isAcceptedIssuerReference() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIssuerUsesACMESpec(t *testing.T) {
	tests := []struct {
		name      string
		spec      map[string]interface{}
		wantACME  bool
		wantError string
	}{
		{
			name:     "absent acme",
			spec:     map[string]interface{}{"ca": map[string]interface{}{"secretName": "ca-secret"}},
			wantACME: false,
		},
		{
			name: "present acme",
			spec: map[string]interface{}{
				"acme": map[string]interface{}{"email": "test@example.com"},
			},
			wantACME: true,
		},
		{
			name:     "empty acme map",
			spec:     map[string]interface{}{"acme": map[string]interface{}{}},
			wantACME: true,
		},
		{
			name:      "wrong type acme",
			spec:      map[string]interface{}{"acme": "acme-string"},
			wantError: "failed to read issuer spec.acme",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer := unstructuredIssuerWithSpec(t, tt.spec)
			got, err := issuerUsesACMESpec(issuer)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("issuerUsesACMESpec() err = %v, want error containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("issuerUsesACMESpec() unexpected error: %v", err)
			}
			if got != tt.wantACME {
				t.Fatalf("issuerUsesACMESpec() = %v, want %v", got, tt.wantACME)
			}
		})
	}
}

func TestIssuerCASecretName(t *testing.T) {
	tests := []struct {
		name      string
		spec      map[string]interface{}
		wantName  string
		wantFound bool
		wantError string
	}{
		{
			name:      "absent ca",
			spec:      map[string]interface{}{"acme": map[string]interface{}{"email": "test@example.com"}},
			wantFound: false,
		},
		{
			name:      "present secretName",
			spec:      map[string]interface{}{"ca": map[string]interface{}{"secretName": "issuer-ca"}},
			wantName:  "issuer-ca",
			wantFound: true,
		},
		{
			name:      "empty secretName",
			spec:      map[string]interface{}{"ca": map[string]interface{}{"secretName": ""}},
			wantName:  "",
			wantFound: true,
		},
		{
			name:      "wrong type secretName",
			spec:      map[string]interface{}{"ca": map[string]interface{}{"secretName": 42}},
			wantError: "failed to read issuer spec.ca.secretName",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer := unstructuredIssuerWithSpec(t, tt.spec)
			gotName, gotFound, err := issuerCASecretName(issuer)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("issuerCASecretName() err = %v, want error containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("issuerCASecretName() unexpected error: %v", err)
			}
			if gotName != tt.wantName {
				t.Fatalf("issuerCASecretName() name = %q, want %q", gotName, tt.wantName)
			}
			if gotFound != tt.wantFound {
				t.Fatalf("issuerCASecretName() found = %v, want %v", gotFound, tt.wantFound)
			}
		})
	}
}

func unstructuredIssuerWithSpec(t *testing.T, spec map[string]interface{}) *unstructured.Unstructured {
	t.Helper()
	issuer := &unstructured.Unstructured{}
	issuer.SetUnstructuredContent(map[string]interface{}{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "Issuer",
		"metadata": map[string]interface{}{
			"name": "test-issuer",
		},
		"spec": spec,
	})
	return issuer
}
