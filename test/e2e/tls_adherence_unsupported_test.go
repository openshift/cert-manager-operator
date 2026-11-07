//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestIsTLSAdherenceUnsupported(t *testing.T) {
	gk := schema.GroupKind{Group: "config.openshift.io", Kind: "APIServer"}

	unsupported := apierrors.NewInvalid(gk, "cluster", field.ErrorList{
		field.NotSupported(field.NewPath("spec", "tlsAdherence"), "StrictAllComponents", []string{}),
	})
	unknownField := apierrors.NewInvalid(gk, "cluster", field.ErrorList{
		field.Invalid(field.NewPath("spec", "tlsAdherence"), "StrictAllComponents", `strict decoding error: unknown field "spec.tlsAdherence"`),
	})
	unrelatedInvalid := apierrors.NewInvalid(gk, "cluster", field.ErrorList{
		field.Invalid(field.NewPath("spec", "tlsSecurityProfile", "type"), "Bogus", "unsupported type"),
	})
	forbidden := apierrors.NewForbidden(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "cluster", fmt.Errorf("update denied"))
	invalidValue := apierrors.NewInvalid(gk, "cluster", field.ErrorList{
		field.Invalid(field.NewPath("spec", "tlsAdherence"), "nope", "must be a valid policy"),
	})
	updateBadRequest := apierrors.NewBadRequest(`APIServer in version "v1" cannot be handled as a APIServer: strict decoding error: unknown field "spec.tlsAdherence"`)
	unrelatedBadRequest := apierrors.NewBadRequest(`the object provided is unrecognized (must be of type APIServer): json parse error`)
	badRequestMentionsField := apierrors.NewBadRequest(`spec.tlsAdherence: Invalid value: "nope"`)

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "field value not supported", err: unsupported, want: true},
		{name: "unknown field", err: unknownField, want: true},
		{name: "wrapped unsupported", err: fmt.Errorf("update apiserver: %w", unsupported), want: true},
		{name: "update bad request unknown field", err: updateBadRequest, want: true},
		{name: "wrapped update bad request", err: fmt.Errorf("update apiserver: %w", updateBadRequest), want: true},
		{name: "unrelated invalid", err: unrelatedInvalid, want: false},
		{name: "forbidden", err: forbidden, want: false},
		{name: "invalid value on tlsAdherence", err: invalidValue, want: false},
		{name: "plain error mentioning field", err: fmt.Errorf("tlsAdherence is unsupported"), want: false},
		{name: "unrelated bad request", err: unrelatedBadRequest, want: false},
		{name: "bad request mentioning field without unknown-field text", err: badRequestMentionsField, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTLSAdherenceUnsupported(tc.err); got != tc.want {
				t.Fatalf("isTLSAdherenceUnsupported() = %v, want %v", got, tc.want)
			}
		})
	}
}
