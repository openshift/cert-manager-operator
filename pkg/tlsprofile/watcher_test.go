package tlsprofile

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestSecurityProfileWatcherHandle(t *testing.T) {
	intermediatePtr, err := EffectiveSpec(nil)
	if err != nil {
		t.Fatal(err)
	}
	intermediate := *intermediatePtr
	modernPtr, err := EffectiveSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType})
	if err != nil {
		t.Fatal(err)
	}
	modern := *modernPtr

	modernAPI := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
		},
	}
	oldAPI := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileOldType,
			},
		},
	}
	strictIntermediateAPI := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
		},
	}
	legacyModernAPI := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
		},
	}
	legacyIntermediateAPI := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
		},
	}
	unresolvableCustomAPI := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
			},
		},
	}
	legacyUnresolvableAPI := &configv1.APIServer{
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
			},
		},
	}
	unrelatedSpecAPI := strictIntermediateAPI.DeepCopy()
	unrelatedSpecAPI.Spec.Encryption.Type = configv1.EncryptionTypeAESCBC
	unrelatedSpecAPI.ResourceVersion = "2"

	tests := []struct {
		name                string
		initialSpec         configv1.TLSProfileSpec
		initialAdherence    configv1.TLSAdherencePolicy
		initialUnresolvable bool
		handles             []*configv1.APIServer
		wantOnChangeCount   int
	}{
		{
			name:              "no change does not fire OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyNoOpinion,
			handles:           []*configv1.APIServer{{}},
			wantOnChangeCount: 0,
		},
		{
			name:              "profile change fires OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyStrictAllComponents,
			handles:           []*configv1.APIServer{modernAPI},
			wantOnChangeCount: 1,
		},
		{
			name:              "adherence change fires OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			handles:           []*configv1.APIServer{strictIntermediateAPI},
			wantOnChangeCount: 1,
		},
		{
			name:              "profile change under legacy adherence fires OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			handles:           []*configv1.APIServer{legacyModernAPI},
			wantOnChangeCount: 1,
		},
		{
			name:              "adherence change with the same profile fires OnChange",
			initialSpec:       modern,
			initialAdherence:  configv1.TLSAdherencePolicyStrictAllComponents,
			handles:           []*configv1.APIServer{legacyModernAPI},
			wantOnChangeCount: 1,
		},
		{
			name:              "profile and adherence changing together fires OnChange once",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			handles:           []*configv1.APIServer{modernAPI},
			wantOnChangeCount: 1,
		},
		{
			name:              "unrelated spec and status updates do not fire OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyStrictAllComponents,
			handles:           []*configv1.APIServer{unrelatedSpecAPI},
			wantOnChangeCount: 0,
		},
		{
			name:              "unresolvable live config fires OnChange",
			initialSpec:       intermediate,
			initialAdherence:  configv1.TLSAdherencePolicyStrictAllComponents,
			handles:           []*configv1.APIServer{unresolvableCustomAPI},
			wantOnChangeCount: 1,
		},
		{
			name:                "still unresolvable with the same adherence does not fire OnChange",
			initialAdherence:    configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			initialUnresolvable: true,
			handles:             []*configv1.APIServer{legacyUnresolvableAPI},
			wantOnChangeCount:   0,
		},
		{
			name:                "unresolvable seed becomes resolvable and fires OnChange",
			initialAdherence:    configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			initialUnresolvable: true,
			handles:             []*configv1.APIServer{legacyIntermediateAPI},
			wantOnChangeCount:   1,
		},
		{
			name:                "adherence change while still unresolvable fires OnChange",
			initialAdherence:    configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			initialUnresolvable: true,
			handles:             []*configv1.APIServer{unresolvableCustomAPI},
			wantOnChangeCount:   1,
		},
		{
			name:             "OnChange fires at most once",
			initialSpec:      intermediate,
			initialAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			handles: []*configv1.APIServer{
				modernAPI,
				oldAPI,
				unresolvableCustomAPI,
			},
			wantOnChangeCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fireCount int
			w := &SecurityProfileWatcher{
				InitialTLSProfileSpec:     tt.initialSpec,
				InitialTLSAdherencePolicy: tt.initialAdherence,
				InitialUnresolvable:       tt.initialUnresolvable,
				OnChange:                  func() { fireCount++ },
			}
			for _, api := range tt.handles {
				w.handle(api)
			}
			if fireCount != tt.wantOnChangeCount {
				t.Fatalf("OnChange fired %d times, want %d", fireCount, tt.wantOnChangeCount)
			}
		})
	}
}
