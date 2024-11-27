//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/openshift/cert-manager-operator/test/library"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	"github.com/stretchr/testify/require"
)

// TestPerformAWSSTSAmbientPreps performs steps to prepare for ACME dns-01 to run
// on an AWS STS with ambient STS credentials. Assumes that the cluster is using
// credentials mode Manual, using an OIDC STS provider, and the "aws-creds" secret
// to be present in the cert-manager namespace which contains a generated AWS role ARN
// suitable for use with AWS Route53.
func TestPerformAWSSTSAmbientPreps(t *testing.T) {
	ctx := context.Background()
	loader := library.NewDynamicResourceLoader(ctx, t)
	config, err := library.GetConfigForTest(t)
	require.NoError(t, err)

	configClient, err := configv1client.NewForConfig(config)
	require.NoError(t, err)

	opClient, err := operatorv1client.NewForConfig(config)
	require.NoError(t, err)

	isSTS, err := isAWSSTSCluster(ctx, opClient, configClient)
	require.NoError(t, err)
	if isSTS {
		err = annotateSAForSTSAndRestartCertManagerPod(ctx, loader)
		require.NoError(t, err)
	}
}
