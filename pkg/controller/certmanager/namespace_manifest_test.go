package certmanager

import (
	"testing"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/stretchr/testify/require"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

// The openshift.io/cluster-monitoring label opts a namespace out of user workload
// monitoring, which is how cert-manager operand metrics are collected
// (docs/operand_metrics.md), so the namespace must carry neither the label nor
// the obsolete annotation.
func TestNamespaceManifestHasNoClusterMonitoringKey(t *testing.T) {
	manifestBytes, err := assets.Asset("cert-manager-deployment/cert-manager-namespace.yaml")
	require.NoError(t, err, "failed to load namespace asset")

	ns := resourceread.ReadNamespaceV1OrDie(manifestBytes)

	require.Equal(t, "cert-manager", ns.Name)
	require.NotContains(t, ns.Labels, "openshift.io/cluster-monitoring")
	require.NotContains(t, ns.Annotations, "openshift.io/cluster-monitoring")
}
