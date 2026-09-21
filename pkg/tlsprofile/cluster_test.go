package tlsprofile

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	configv1 "github.com/openshift/api/config/v1"
)

func TestResolveHonoredTLSProfile_fetchErrors(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: configv1.GroupName, Resource: "apiservers"}, APIServerClusterName)
	forbidden := apierrors.NewForbidden(schema.GroupResource{Group: configv1.GroupName, Resource: "apiservers"}, APIServerClusterName, errors.New("denied"))

	t.Run("NotFound is soft skip", func(t *testing.T) {
		spec, err := ResolveHonoredTLSProfile(context.Background(), func(context.Context) (*configv1.APIServer, error) {
			return nil, notFound
		}, "test", FetchErrorPropagateExceptNotFound)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spec != nil {
			t.Fatalf("expected nil spec on NotFound, got %#v", spec)
		}
	})

	t.Run("Forbidden propagates", func(t *testing.T) {
		spec, err := ResolveHonoredTLSProfile(context.Background(), func(context.Context) (*configv1.APIServer, error) {
			return nil, forbidden
		}, "test", FetchErrorPropagateExceptNotFound)
		if err == nil {
			t.Fatal("expected error")
		}
		if spec != nil {
			t.Fatalf("expected nil spec on Forbidden, got %#v", spec)
		}
	})

	t.Run("nil fetch function errors", func(t *testing.T) {
		_, err := ResolveHonoredTLSProfile(context.Background(), nil, "test", FetchErrorPropagateExceptNotFound)
		if err == nil {
			t.Fatal("expected error for nil fetch")
		}
	})
}

func TestResolveHonoredTLSProfile_adherence(t *testing.T) {
	wantModern, err := EffectiveSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType})
	if err != nil {
		t.Fatal(err)
	}
	wantIntermediate, err := EffectiveSpec(nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		apiServer *configv1.APIServer
		wantSpec  *configv1.TLSProfileSpec
		wantErr   string
	}{
		{
			name: "empty adherence skips",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
				},
			},
		},
		{
			name: "legacy adherence skips",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSAdherence:       configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
				},
			},
		},
		{
			// TLSAdherencePolicyNoOpinion is ""; explicit const documents operator-start soft-skip.
			name: "noOpinion adherence skips",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSAdherence:       configv1.TLSAdherencePolicyNoOpinion,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
				},
			},
		},
		{
			name: "strict modern returns effective spec",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSAdherence:       configv1.TLSAdherencePolicyStrictAllComponents,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
				},
			},
			wantSpec: wantModern,
		},
		{
			name: "unknown adherence treated as strict with nil profile falls back to Intermediate",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicy("FutureStrictMode"),
				},
			},
			wantSpec: wantIntermediate,
		},
		{
			name: "strict with invalid custom profile propagates error",
			apiServer: &configv1.APIServer{
				Spec: configv1.APIServerSpec{
					TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
					TLSSecurityProfile: &configv1.TLSSecurityProfile{
						Type: configv1.TLSProfileCustomType,
					},
				},
			},
			wantErr: "custom TLS profile is missing custom settings",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveHonoredTLSProfile(context.Background(), func(context.Context) (*configv1.APIServer, error) {
				return tc.apiServer, nil
			}, "test", FetchErrorPropagateExceptNotFound)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
				}
				if got != nil {
					t.Fatalf("expected nil spec on error, got %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantSpec == nil {
				if got != nil {
					t.Fatalf("expected nil spec, got %#v", got)
				}
				return
			}
			if got.MinTLSVersion != tc.wantSpec.MinTLSVersion {
				t.Fatalf("MinTLSVersion = %q, want %q", got.MinTLSVersion, tc.wantSpec.MinTLSVersion)
			}
			if !reflect.DeepEqual(got.Ciphers, tc.wantSpec.Ciphers) {
				t.Fatalf("Ciphers = %#v, want %#v", got.Ciphers, tc.wantSpec.Ciphers)
			}
		})
	}
}

func TestNewRESTConfigAPIServerFetch_nilConfig(t *testing.T) {
	_, err := NewRESTConfigAPIServerFetch(nil)
	if err == nil {
		t.Fatal("expected error for nil rest config")
	}
}

func TestClusterAPIServerTLSConfigChanged(t *testing.T) {
	modern := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	intermediate := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	customA := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
			},
		},
	}
	customB := customA.DeepCopy()
	customB.Custom.Ciphers = []string{"TLS_AES_256_GCM_SHA384"}

	base := func(adherence configv1.TLSAdherencePolicy, profile *configv1.TLSSecurityProfile) *configv1.APIServer {
		return &configv1.APIServer{
			Spec: configv1.APIServerSpec{
				TLSAdherence:       adherence,
				TLSSecurityProfile: profile,
			},
		}
	}

	tests := []struct {
		name string
		old  *configv1.APIServer
		new  *configv1.APIServer
		want bool
	}{
		{name: "both nil"},
		{
			name: "old nil",
			new:  base(configv1.TLSAdherencePolicyStrictAllComponents, modern),
			want: true,
		},
		{
			name: "new nil",
			old:  base(configv1.TLSAdherencePolicyStrictAllComponents, modern),
			want: true,
		},
		{
			name: "unchanged TLS fields",
			old:  base(configv1.TLSAdherencePolicyStrictAllComponents, modern),
			new:  base(configv1.TLSAdherencePolicyStrictAllComponents, modern),
		},
		{
			name: "adherence changed",
			old:  base(configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly, modern),
			new:  base(configv1.TLSAdherencePolicyStrictAllComponents, modern),
			want: true,
		},
		{
			name: "profile type changed",
			old:  base(configv1.TLSAdherencePolicyStrictAllComponents, intermediate),
			new:  base(configv1.TLSAdherencePolicyStrictAllComponents, modern),
			want: true,
		},
		{
			name: "custom cipher list changed",
			old:  base(configv1.TLSAdherencePolicyStrictAllComponents, customA),
			new:  base(configv1.TLSAdherencePolicyStrictAllComponents, customB),
			want: true,
		},
		{
			name: "status-only update",
			old: func() *configv1.APIServer {
				a := base(configv1.TLSAdherencePolicyStrictAllComponents, modern)
				a.ResourceVersion = "1"
				return a
			}(),
			new: func() *configv1.APIServer {
				a := base(configv1.TLSAdherencePolicyStrictAllComponents, modern)
				a.ResourceVersion = "2"
				return a
			}(),
		},
		{
			name: "unrelated encryption spec change",
			old:  base(configv1.TLSAdherencePolicyStrictAllComponents, modern),
			new: func() *configv1.APIServer {
				a := base(configv1.TLSAdherencePolicyStrictAllComponents, modern)
				a.Spec.Encryption.Type = configv1.EncryptionTypeAESCBC
				return a
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClusterAPIServerTLSConfigChanged(tt.old, tt.new); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
