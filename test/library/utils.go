//go:build e2e
// +build e2e

package library

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
)

func (d DynamicResourceLoader) CreateTestingNS(namespacePrefix string) (*v1.Namespace, error) {
	t := testing.T{}
	namespace := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%v-", namespacePrefix),
			Labels: map[string]string{
				"e2e-test": "true",
				"operator": "openshift-cert-manager-operator",
			},
		},
	}

	var got *v1.Namespace
	if err := wait.PollImmediate(1*time.Second, 30*time.Second, func() (bool, error) {
		var err error
		got, err = d.KubeClient.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
		if err != nil {
			t.Logf("Error creating namespace: %v", err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	return got, nil
}

func (d DynamicResourceLoader) DeleteTestingNS(name string) (bool, error) {
	t := testing.T{}
	ctx := context.Background()

	err := d.KubeClient.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		t.Logf("Namespace: %v not found, err: %v", name, err)
	}

	if err := wait.PollImmediate(1*time.Second, 30*time.Second, func() (bool, error) {

		// Poll until namespace is deleted
		ns, err := d.KubeClient.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		t.Logf("Namespace: %v", ns)
		if err != nil {
			t.Logf("Error getting namespace: %v", err)
			if k8serrors.IsNotFound(err) {
				return true, err
			}
			return false, nil
		}
		return false, nil
	}); err != nil {
		t.Logf("Error getting namespace: %v", err)
		return true, err
	}
	return false, nil
}

func GetClusterBaseDomain(ctx context.Context, configClient configv1.ConfigV1Interface) (string, error) {
	dns, err := configClient.DNSes().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return dns.Spec.BaseDomain, nil
}
