package operator

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

// stubReader is a minimal client.Reader that serves a single APIServer object
// or returns a fixed error from Get.
type stubReader struct {
	apiServer *configv1.APIServer
	getErr    error
}

func (s *stubReader) Get(_ context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	if s.getErr != nil {
		return s.getErr
	}
	out, ok := obj.(*configv1.APIServer)
	if !ok {
		return fmt.Errorf("stubReader: unexpected object type %T", obj)
	}
	s.apiServer.DeepCopyInto(out)
	return nil
}

func (s *stubReader) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return errors.New("stubReader: List not implemented")
}

func apiServerWith(adherence configv1.TLSAdherencePolicy, profile *configv1.TLSSecurityProfile) *configv1.APIServer {
	return &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence:       adherence,
			TLSSecurityProfile: profile,
		},
	}
}

func mustEffectiveSpec(t *testing.T, profile *configv1.TLSSecurityProfile) *configv1.TLSProfileSpec {
	t.Helper()
	spec, err := tlsprofile.EffectiveSpec(profile)
	if err != nil {
		t.Fatalf("EffectiveSpec(%v): %v", profile, err)
	}
	return spec
}

func TestResolveIstioCSRTLSConfig(t *testing.T) {
	modern := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}

	tests := []struct {
		name    string
		reader  *stubReader
		want    istiocsr.TLSConfig
		wantErr string
	}{
		{
			name:   "apiserver/cluster not found uses default TLS config",
			reader: &stubReader{getErr: apierrors.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "apiservers"}, "cluster")},
			want:   istiocsr.TLSConfig{},
		},
		{
			name:    "apiserver/cluster read error is returned",
			reader:  &stubReader{getErr: errors.New("connection refused")},
			wantErr: "failed to get apiserver.config.openshift.io/cluster",
		},
		{
			name:   "empty tlsAdherence does not honor the cluster profile",
			reader: &stubReader{apiServer: apiServerWith(configv1.TLSAdherencePolicyNoOpinion, modern)},
			want:   istiocsr.TLSConfig{Adherence: configv1.TLSAdherencePolicyNoOpinion, Honor: false},
		},
		{
			name:   "LegacyAdheringComponentsOnly does not honor the cluster profile",
			reader: &stubReader{apiServer: apiServerWith(configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly, modern)},
			want:   istiocsr.TLSConfig{Adherence: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly, Honor: false},
		},
		{
			name:   "StrictAllComponents with no profile resolves to Intermediate",
			reader: &stubReader{apiServer: apiServerWith(configv1.TLSAdherencePolicyStrictAllComponents, nil)},
			want: istiocsr.TLSConfig{
				Adherence: configv1.TLSAdherencePolicyStrictAllComponents,
				Spec:      mustEffectiveSpec(t, &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}),
				Honor:     true,
			},
		},
		{
			name:   "StrictAllComponents with Modern profile resolves to Modern",
			reader: &stubReader{apiServer: apiServerWith(configv1.TLSAdherencePolicyStrictAllComponents, modern)},
			want: istiocsr.TLSConfig{
				Adherence: configv1.TLSAdherencePolicyStrictAllComponents,
				Spec:      mustEffectiveSpec(t, modern),
				Honor:     true,
			},
		},
		{
			name:   "unknown tlsAdherence is treated as StrictAllComponents",
			reader: &stubReader{apiServer: apiServerWith("FutureAdherence", modern)},
			want: istiocsr.TLSConfig{
				Adherence: "FutureAdherence",
				Spec:      mustEffectiveSpec(t, modern),
				Honor:     true,
			},
		},
		{
			name: "StrictAllComponents with Custom profile missing settings fails",
			reader: &stubReader{apiServer: apiServerWith(configv1.TLSAdherencePolicyStrictAllComponents,
				&configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType})},
			wantErr: "failed to resolve effective TLS profile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveIstioCSRTLSConfig(tt.reader)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolveIstioCSRTLSConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
