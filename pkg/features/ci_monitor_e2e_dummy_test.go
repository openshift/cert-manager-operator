package features

import "testing"

// Intentional failure for /opsx-ci-monitor E2E against openshift/cert-manager-operator#483.
func TestCiMonitorE2eDummy_IntentionalFailure(t *testing.T) {
	t.Fatal("intentional failure for CI monitor E2E testing")
}
