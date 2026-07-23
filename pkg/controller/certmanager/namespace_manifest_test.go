package certmanager

import (
	"testing"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/stretchr/testify/require"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func TestNamespaceManifestHasClusterMonitoringLabel(t *testing.T) {
	manifestBytes, err := assets.Asset("cert-manager-deployment/cert-manager-namespace.yaml")
	require.NoError(t, err, "failed to load namespace asset")

	ns := resourceread.ReadNamespaceV1OrDie(manifestBytes)

	require.Equal(t, "cert-manager", ns.Name)
	require.Equal(t, "true", ns.Labels["openshift.io/cluster-monitoring"],
		"openshift.io/cluster-monitoring must be a label (not annotation) for Prometheus namespace discovery")
}
