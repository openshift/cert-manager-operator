package e2e

import (
	"context"
	"embed"
	"path/filepath"
	"testing"
	"time"

	_ "embed"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/cert-manager-operator/test/library"
)

const (
	PollInterval = time.Second
	TestTimeout  = 10 * time.Minute
)

//go:embed testdata/*
var testassets embed.FS

func TestSelfSignedCerts(t *testing.T) {
	loader := library.NewDynamicResourceLoader(context.TODO(), t)

	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "namespace.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "namespace.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"))
	defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"))
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"))
	defer loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"))

	err := wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
		// TODO: The loader.KubeClient might be worth splitting out. Let's see once we have more tests.
		secret, err := loader.KubeClient.CoreV1().Secrets("sandbox").Get(context.TODO(), "root-secret", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			t.Logf("Unable to retrieve the root secret: %v", err)
			return false, nil
		}
		if err != nil {
			return false, err
		}

		return len(secret.Data["ca.crt"]) != 0 && len(secret.Data["tls.crt"]) != 0 && len(secret.Data["tls.key"]) != 0, nil
	})
	require.NoError(t, err)
}
