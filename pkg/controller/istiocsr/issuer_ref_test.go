package istiocsr

import (
	"testing"

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
