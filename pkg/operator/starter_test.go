package operator

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	configv1 "github.com/openshift/api/config/v1"
	configfake "github.com/openshift/client-go/config/clientset/versioned/fake"

	"github.com/openshift/cert-manager-operator/pkg/features"
)

// newFakeConfigClient mirrors the helper in pkg/features: when withFeatureGateAPI
// is true, FakeDiscovery serves featuregates for config.openshift.io/v1.
func newFakeConfigClient(withFeatureGateAPI bool, objs ...runtime.Object) *configfake.Clientset {
	cs := configfake.NewClientset(objs...)
	if withFeatureGateAPI {
		cs.Resources = []*metav1.APIResourceList{
			{
				GroupVersion: configv1.GroupVersion.String(),
				APIResources: []metav1.APIResource{{Name: "featuregates", Namespaced: false, Kind: "FeatureGate"}},
			},
		}
	}
	return cs
}

func clusterFeatureGate(featureSet configv1.FeatureSet) *configv1.FeatureGate {
	return &configv1.FeatureGate{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: configv1.FeatureGateSpec{
			FeatureGateSelection: configv1.FeatureGateSelection{FeatureSet: featureSet},
		},
	}
}

// The retry path that exhausts all attempts is not covered here: it waits
// 30s between attempts. The cancelled-context case covers the retry wait.
// Cases share the process-wide operator featuregate, so they run in order.
// The invalid-flag case is in TestSetupFeatureGatesRejectsInvalidFlag.
func TestSetupFeatureGates(t *testing.T) {
	origFlags := UnsupportedAddonFeatures
	t.Cleanup(func() {
		UnsupportedAddonFeatures = origFlags
		// Restore operator featuregate defaults for other tests in this package.
		if err := features.SetupWithFlagValue("IstioCSR=true,TrustManager=false"); err != nil {
			t.Errorf("restoring operator featuregate defaults: %v", err)
		}
	})

	tests := []struct {
		name    string
		flags   string
		client  func() *configfake.Clientset
		ctx     func() context.Context
		wantErr string
	}{
		{
			name:  "featuregate API served with preview featureset",
			flags: "TrustManager=true",
			client: func() *configfake.Clientset {
				return newFakeConfigClient(true, clusterFeatureGate(configv1.TechPreviewNoUpgrade))
			},
		},
		{
			name:   "featuregate API not served",
			client: func() *configfake.Clientset { return newFakeConfigClient(false) },
		},
		{
			name: "discovery failure with cancelled context stops retrying",
			client: func() *configfake.Clientset {
				cs := newFakeConfigClient(true)
				cs.AddReactor("get", "resource", func(clientgotesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("simulated discovery failure")
				})
				return cs
			},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantErr: "context cancelled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			UnsupportedAddonFeatures = tt.flags
			ctx := context.Background()
			if tt.ctx != nil {
				ctx = tt.ctx()
			}

			start := time.Now()
			state, err := setupFeatureGates(ctx, tt.client())
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("setupFeatureGates took %s; expected no retry wait", elapsed)
			}

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if state == nil {
				t.Fatal("expected non-nil FeatureGateState")
			}
			if state.Err() != nil {
				t.Errorf("expected no state error, got %v", state.Err())
			}
		})
	}
}

// invalidFlagChildEnv marks the subprocess that runs the invalid-flag case.
const invalidFlagChildEnv = "CERT_MANAGER_OPERATOR_TEST_INVALID_ADDON_FLAG"

// TestSetupFeatureGatesRejectsInvalidFlag runs the Bogus=true case in a
// subprocess. The featuregate records unknown keys in its raw state, so after
// a failed Set every later Set on the process-wide gate also fails. Running it
// in this process would break the other featuregate tests, including on
// go test -count=2.
func TestSetupFeatureGatesRejectsInvalidFlag(t *testing.T) {
	if os.Getenv(invalidFlagChildEnv) == "1" {
		UnsupportedAddonFeatures = "Bogus=true"
		const wantErr = "failed to parse addon features"
		_, err := setupFeatureGates(context.Background(), newFakeConfigClient(true))
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("expected error containing %q, got %v", wantErr, err)
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSetupFeatureGatesRejectsInvalidFlag$", "-test.count=1") //nolint:gosec // re-executes this test binary
	cmd.Env = append(os.Environ(), invalidFlagChildEnv+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("invalid-flag subprocess failed: %v\n%s", err, out)
	}
}
