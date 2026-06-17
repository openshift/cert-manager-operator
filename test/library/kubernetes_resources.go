//go:build e2e

package library

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func populateSecretData(secret *corev1.Secret) {
	if len(secret.Data) == 0 && len(secret.StringData) > 0 {
		secret.Data = make(map[string][]byte, len(secret.StringData))
		for k, v := range secret.StringData {
			secret.Data[k] = []byte(v)
		}
		secret.StringData = nil
	}
}

// UpsertSecret creates the secret or updates it when it already exists so data matches desired.
func UpsertSecret(ctx context.Context, client kubernetes.Interface, secret *corev1.Secret) error {
	populateSecretData(secret)

	_, err := client.CoreV1().Secrets(secret.Namespace).Create(ctx, secret.DeepCopy(), metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !k8serrors.IsAlreadyExists(err) {
		return err
	}

	existing, getErr := client.CoreV1().Secrets(secret.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if getErr != nil {
		return getErr
	}

	updated := secret.DeepCopy()
	updated.ResourceVersion = existing.ResourceVersion
	populateSecretData(updated)
	_, err = client.CoreV1().Secrets(secret.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
	return err
}

// UpsertConfigMap creates the ConfigMap or updates it when it already exists so data matches desired.
func UpsertConfigMap(ctx context.Context, client kubernetes.Interface, configMap *corev1.ConfigMap) error {
	_, err := client.CoreV1().ConfigMaps(configMap.Namespace).Create(ctx, configMap.DeepCopy(), metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !k8serrors.IsAlreadyExists(err) {
		return err
	}

	existing, getErr := client.CoreV1().ConfigMaps(configMap.Namespace).Get(ctx, configMap.Name, metav1.GetOptions{})
	if getErr != nil {
		return getErr
	}

	updated := configMap.DeepCopy()
	updated.ResourceVersion = existing.ResourceVersion
	_, err = client.CoreV1().ConfigMaps(configMap.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
	return err
}
