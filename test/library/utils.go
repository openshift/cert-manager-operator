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
)

func (d DynamicResourceLoader) CreateTestingNS(baseName string) (*v1.Namespace, error) {
	name := fmt.Sprintf("%v", baseName)
	t := testing.T{}
	namespaceObj := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "",
		},
		Status: v1.NamespaceStatus{},
	}

	var got *v1.Namespace
	if err := wait.PollImmediate(1*time.Second, 30*time.Second, func() (bool, error) {
		var err error
		got, err = d.KubeClient.CoreV1().Namespaces().Create(context.Background(), namespaceObj, metav1.CreateOptions{})
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

func (d DynamicResourceLoader) DeleteTestingNS(baseName string) (bool, error) {
	name := fmt.Sprintf("%v", baseName)
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
