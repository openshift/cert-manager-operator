package operator

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/openshift/cert-manager-operator/pkg/features"
)

func TestSetupFeatureGates(t *testing.T) {
	origFlags := UnsupportedAddonFeatures
	t.Cleanup(func() {
		UnsupportedAddonFeatures = origFlags
		// Restore operator featuregate defaults for other tests in this package.
		if err := features.SetupWithFlagValue("IstioCSR=true,TrustManager=true"); err != nil {
			t.Errorf("restoring operator featuregate defaults: %v", err)
		}
	})

	tests := []struct {
		name  string
		flags string
	}{
		{
			name:  "parses TrustManager enablement",
			flags: "TrustManager=true",
		},
		{
			name:  "empty flags leave defaults",
			flags: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			UnsupportedAddonFeatures = tt.flags
			if err := setupFeatureGates(); err != nil {
				t.Fatalf("unexpected error: %v", err)
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
		err := setupFeatureGates()
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
