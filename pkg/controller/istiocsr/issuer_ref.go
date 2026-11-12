package istiocsr

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

const (
	defaultIssuerKind  = "Issuer"
	defaultIssuerGroup = "cert-manager.io"
)

// errUnsupportedIssuerRefKind indicates issuerRef does not reference an accepted issuer kind.
var errUnsupportedIssuerRefKind = errors.New("unsupported issuerRef kind")

// isAcceptedIssuerReference reports whether issuerRef points at a cert-manager or third-party issuer type.
// It rejects arbitrary API kinds (for example apps/Deployment) that REST mapping alone would allow.
func isAcceptedIssuerReference(ref certmanagermetav1.IssuerReference) bool {
	ref = resolvedIssuerRef(ref)
	if isCertManagerBuiltInIssuer(ref) {
		return true
	}
	return strings.HasSuffix(strings.ToLower(ref.Kind), "issuer")
}

// resolvedIssuerRef returns issuerRef with default kind and group applied.
func resolvedIssuerRef(ref certmanagermetav1.IssuerReference) certmanagermetav1.IssuerReference {
	resolved := ref
	if resolved.Kind == "" {
		resolved.Kind = defaultIssuerKind
	}
	if resolved.Group == "" {
		resolved.Group = defaultIssuerGroup
	}
	return resolved
}

func (r *Reconciler) getIssuer(istiocsr *v1alpha1.IstioCSR) (*unstructured.Unstructured, error) {
	ref := resolvedIssuerRef(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef)
	if !isAcceptedIssuerReference(ref) {
		return nil, fmt.Errorf("%w %q", errUnsupportedIssuerRefKind, ref.Kind)
	}

	mapping, err := r.restMapper.RESTMapping(schema.GroupKind{Group: ref.Group, Kind: ref.Kind})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve issuer reference %q/%q: %w", ref.Group, ref.Kind, err)
	}

	key := client.ObjectKey{Name: ref.Name}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		key.Namespace = istiocsr.Spec.IstioCSRConfig.Istio.Namespace
	}

	issuer := &unstructured.Unstructured{}
	issuer.SetGroupVersionKind(mapping.GroupVersionKind)
	if err := r.apiReader.Get(r.ctx, key, issuer); err != nil {
		return nil, fmt.Errorf("failed to fetch issuer %q: %w", key, err)
	}
	return issuer, nil
}

func isCertManagerBuiltInIssuer(ref certmanagermetav1.IssuerReference) bool {
	ref = resolvedIssuerRef(ref)
	group := strings.ToLower(ref.Group)
	kind := strings.ToLower(ref.Kind)
	return group == issuerGroup && (kind == issuerKind || kind == clusterIssuerKind)
}

func issuerUsesACMESpec(issuer *unstructured.Unstructured) (bool, error) {
	acme, found, err := unstructured.NestedMap(issuer.Object, "spec", "acme")
	if err != nil {
		return false, fmt.Errorf("failed to read issuer spec.acme: %w", err)
	}
	return found && acme != nil, nil
}

func issuerCASecretName(issuer *unstructured.Unstructured) (string, bool, error) {
	secretName, found, err := unstructured.NestedString(issuer.Object, "spec", "ca", "secretName")
	if err != nil {
		return "", false, fmt.Errorf("failed to read issuer spec.ca.secretName: %w", err)
	}
	if !found {
		return "", false, nil
	}
	return secretName, true, nil
}

// issuerRefForIstioCSR returns the effective issuerRef for an IstioCSR resource.
func issuerRefForIstioCSR(istiocsr *v1alpha1.IstioCSR) certmanagermetav1.IssuerReference {
	return resolvedIssuerRef(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef)
}
