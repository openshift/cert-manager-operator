//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	opv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	"github.com/openshift/cert-manager-operator/test/library"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

var subscriptionSchema = schema.GroupVersionResource{
	Group:    "operators.coreos.com",
	Version:  "v1alpha1",
	Resource: "subscriptions",
}

// verifyOperatorStatusCondition polls every 1 second to check if the status of each of the controllers
// match with the expected conditions. It returns an error if a timeout (5 mins) occurs or an error was
// encountered which polling the status. For each controller a the polling happens in separate go-routines.
func verifyOperatorStatusCondition(client *certmanoperatorclient.Clientset, controllerNames []string, expectedConditionMap map[string]opv1.ConditionStatus) error {

	var wg sync.WaitGroup
	errs := make([]error, len(controllerNames))
	for index := range controllerNames {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			err := wait.PollImmediate(time.Second*1, time.Minute*5, func() (done bool, err error) {
				operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", metav1.GetOptions{})
				if err != nil {
					if apierrors.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}

				if operator.DeletionTimestamp != nil {
					return false, nil
				}

				for _, cond := range operator.Status.Conditions {
					if status, exists := expectedConditionMap[strings.TrimPrefix(cond.Type, controllerNames[index])]; exists {
						if cond.Status != status {
							return false, nil
						}
					}
				}

				return true, nil
			})
			errs[index] = err
		}(index)
	}
	wg.Wait()

	return errors.NewAggregate(errs)
}

// resetCertManagerState is used to revert back to the default cert-manager operands' state
func resetCertManagerState(ctx context.Context, client *certmanoperatorclient.Clientset, loader library.DynamicResourceLoader) error {
	// update operator spec to empty *Config and set operatorSpec to default values
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var operatorState *v1alpha1.CertManager
		err := wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
			operator, err := client.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}

			operatorState = operator
			return true, nil
		})
		if err != nil {
			return err
		}

		updatedOperator := operatorState.DeepCopy()

		updatedOperator.Spec.CAInjectorConfig = nil
		updatedOperator.Spec.ControllerConfig = nil
		updatedOperator.Spec.WebhookConfig = nil
		updatedOperator.Spec.OperatorSpec = opv1.OperatorSpec{
			ManagementState: opv1.Managed,
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})

	// remove any entries present in Subscription spec.config for
	// user provided injected env vars, etc.
	// it should put operator back to default deployment
	subName, err := getCertManagerOperatorSubscription(ctx, loader)
	if err != nil {
		return err
	}

	// to get an empty spec.config
	configPatch := map[string]interface{}{
		"spec": map[string]interface{}{
			"config": nil,
		},
	}
	payload, err := json.Marshal(configPatch)
	if err != nil {
		return err
	}

	subscriptionClient := loader.DynamicClient.Resource(subscriptionSchema).Namespace("cert-manager-operator")
	_, err = subscriptionClient.Patch(ctx, subName, types.MergePatchType, payload, metav1.PatchOptions{})
	return err
}

// addOverrideArgs adds the override args to specific the cert-manager operand. The update process is retried if
// a conflict error is encountered.
func addOverrideArgs(client *certmanoperatorclient.Clientset, deploymentName string, args []string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}

		updatedOperator := operator.DeepCopy()

		switch deploymentName {
		case certmanagerControllerDeployment:
			updatedOperator.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{
				OverrideArgs: args,
			}
		case certmanagerWebhookDeployment:
			updatedOperator.Spec.WebhookConfig = &v1alpha1.DeploymentConfig{
				OverrideArgs: args,
			}
		case certmanagerCAinjectorDeployment:
			updatedOperator.Spec.CAInjectorConfig = &v1alpha1.DeploymentConfig{
				OverrideArgs: args,
			}
		default:
			return fmt.Errorf("unsupported deployment name: %s", deploymentName)
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentArgs polls every 1 second to check if the deployment args list is updated to contain the
// passed args. It returns an error if a timeout (5 mins) occurs or an error was encountered while polling
// the deployment args list.
func verifyDeploymentArgs(k8sclient *kubernetes.Clientset, deploymentName string, args []string, added bool) error {

	return wait.PollImmediate(time.Second*1, time.Minute*5, func() (done bool, err error) {
		controllerDeployment, err := k8sclient.AppsV1().Deployments(operandNamespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		if len(controllerDeployment.Spec.Template.Spec.Containers) == 0 {
			return false, fmt.Errorf("%s deployment spec does not have container information", deploymentName)
		}

		containerArgsSet := sets.New[string](controllerDeployment.Spec.Template.Spec.Containers[0].Args...)

		if added {
			if !containerArgsSet.HasAll(args...) {
				return false, nil
			}
		} else {
			if containerArgsSet.HasAll(args...) {
				return false, nil
			}
		}

		return true, nil
	})
}

// addOverrideResources adds the override resources to the specific cert-manager operand. The update process
// is retried if a conflict error is encountered.
func addOverrideResources(client *certmanoperatorclient.Clientset, deploymentName string, res v1alpha1.CertManagerResourceRequirements) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}

		updatedOperator := operator.DeepCopy()

		switch deploymentName {
		case certmanagerControllerDeployment:
			updatedOperator.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{
				OverrideResources: res,
			}
		case certmanagerWebhookDeployment:
			updatedOperator.Spec.WebhookConfig = &v1alpha1.DeploymentConfig{
				OverrideResources: res,
			}
		case certmanagerCAinjectorDeployment:
			updatedOperator.Spec.CAInjectorConfig = &v1alpha1.DeploymentConfig{
				OverrideResources: res,
			}
		default:
			return fmt.Errorf("unsupported deployment name: %s", deploymentName)
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentResources polls every 10 seconds to check if the deployment resources is updated to contain
// the passed resources. It returns an error if a timeout (5 mins) occurs or an error was encountered while
// polling the deployment resources.
func verifyDeploymentResources(k8sclient *kubernetes.Clientset, deploymentName string, res v1alpha1.CertManagerResourceRequirements, added bool) error {

	return wait.PollImmediate(time.Second*10, time.Minute*5, func() (done bool, err error) {
		controllerDeployment, err := k8sclient.AppsV1().Deployments(operandNamespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		if len(controllerDeployment.Spec.Template.Spec.Containers) == 0 {
			return false, fmt.Errorf("%s deployment spec does not have container information", deploymentName)
		}

		containerResourcesLimits := controllerDeployment.Spec.Template.Spec.Containers[0].Resources.Limits
		equalityLimits := equality.Semantic.DeepEqual(containerResourcesLimits, res.Limits)

		containerResourcesRequests := controllerDeployment.Spec.Template.Spec.Containers[0].Resources.Requests
		equalityRequests := equality.Semantic.DeepEqual(containerResourcesRequests, res.Requests)

		if added {
			if !equalityLimits || !equalityRequests {
				return false, nil
			}
		} else {
			if equalityLimits && equalityRequests {
				return false, nil
			}
		}

		return true, nil
	})
}

// addOverrideScheduling adds the override scheduling to the specific cert-manager operand. The update process
// is retried if a conflict error is encountered.
func addOverrideScheduling(client *certmanoperatorclient.Clientset, deploymentName string, res v1alpha1.CertManagerScheduling) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}

		updatedOperator := operator.DeepCopy()

		switch deploymentName {
		case certmanagerControllerDeployment:
			updatedOperator.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{
				OverrideScheduling: res,
			}
		case certmanagerWebhookDeployment:
			updatedOperator.Spec.WebhookConfig = &v1alpha1.DeploymentConfig{
				OverrideScheduling: res,
			}
		case certmanagerCAinjectorDeployment:
			updatedOperator.Spec.CAInjectorConfig = &v1alpha1.DeploymentConfig{
				OverrideScheduling: res,
			}
		default:
			return fmt.Errorf("unsupported deployment name: %s", deploymentName)
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentScheduling polls every 10 seconds to check if the deployment scheduling is updated to contain
// the passed scheduling. It returns an error if a timeout (5 mins) occurs or an error was encountered while
// polling the deployment scheduling.
func verifyDeploymentScheduling(k8sclient *kubernetes.Clientset, deploymentName string, res v1alpha1.CertManagerScheduling, added bool) error {

	return wait.PollUntilContextTimeout(context.TODO(), time.Second*10, time.Minute*5, true, func(context.Context) (done bool, err error) {
		controllerDeployment, err := k8sclient.AppsV1().Deployments(operandNamespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		podNodeSelector := controllerDeployment.Spec.Template.Spec.NodeSelector
		cmpOptsNodeSelector := cmp.Options{
			// Ignore the node labels which are not part of res.NodeSelector
			// when checking for equality with podNodeSelector.
			cmpopts.IgnoreMapEntries(func(k, v string) bool {
				if actualValue, exists := res.NodeSelector[k]; exists && v == actualValue {
					return false
				}
				return true
			}),
		}
		equalityNodeSelector := cmp.Equal(podNodeSelector, res.NodeSelector, cmpOptsNodeSelector)

		podTolerations := controllerDeployment.Spec.Template.Spec.Tolerations
		tolerationsMap := make(map[corev1.Toleration]bool)
		for _, toleration := range res.Tolerations {
			tolerationsMap[toleration] = true
		}
		cmpOptsTolerations := cmp.Options{
			// Ignore the tolerations which are not part of res.Tolerations
			// when checking for equality with podTolerations.
			cmpopts.IgnoreSliceElements(func(toleration corev1.Toleration) bool {
				if exists := tolerationsMap[toleration]; exists {
					return false
				}
				return true
			}),
		}
		equalityTolerations := cmp.Equal(podTolerations, res.Tolerations, cmpOptsTolerations)

		if added {
			if !equalityNodeSelector || !equalityTolerations {
				return false, nil
			}
		} else {
			if equalityNodeSelector && equalityTolerations {
				return false, nil
			}
		}

		return true, nil
	})
}

// getCertManagerOperatorSubscription returns the name of the first subscription object by listing
// them in the cert-manager-operator namespace using a k8s dynamic client
func getCertManagerOperatorSubscription(ctx context.Context, loader library.DynamicResourceLoader) (string, error) {
	subscriptionClient := loader.DynamicClient.Resource(subscriptionSchema).Namespace("cert-manager-operator")

	subs, err := subscriptionClient.List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	if len(subs.Items) == 0 {
		return "", fmt.Errorf("no subscription object found in operator namespace")
	}

	subName, ok := subs.Items[0].Object["metadata"].(map[string]interface{})["name"].(string)
	if !ok {
		return "", fmt.Errorf("could not parse metadata.name from the first subscription object found")
	}
	return subName, nil
}

// patchSubscriptionWithCloudCredential uses the k8s dynamic client to patche the only Subscription object
// in the cert-manager-operator namespace to inject CLOUD_CREDENTIALS_SECRET_NAME="aws-creds" env
// into its spec.config.env
func patchSubscriptionWithCloudCredential(ctx context.Context, loader library.DynamicResourceLoader, credentialSecret string) error {
	subName, err := getCertManagerOperatorSubscription(ctx, loader)
	if err != nil {
		return err
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"config": map[string]interface{}{
				"env": []interface{}{
					map[string]interface{}{
						"name":  "CLOUD_CREDENTIALS_SECRET_NAME",
						"value": credentialSecret,
					},
				},
			},
		},
	}
	payload, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	subscriptionClient := loader.DynamicClient.Resource(subscriptionSchema).Namespace("cert-manager-operator")
	_, err = subscriptionClient.Patch(ctx, subName, types.MergePatchType, payload, metav1.PatchOptions{})
	return err
}

// waitForCertificateReadiness polls the status of the Certificate object and returns non-nil error
// once the Ready condition is true, otherwise should return a time-out error
func waitForCertificateReadiness(ctx context.Context, certName, namespace string) error {
	return wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
		cert, err := certmanagerClient.CertmanagerV1().Certificates(namespace).Get(ctx, certName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		for _, cond := range cert.Status.Conditions {
			if cond.Type == cmv1.CertificateConditionReady {
				return cond.Status == cmmetav1.ConditionTrue, nil
			}
		}
		return false, nil
	})
}

// verifyCertificate loads the tls secret as a X509 certificate and verifies the following
// - certificate secret is non null, i.e. secret contains "tls.crt", "tls.key" keys
// - certificate hasn't expired
// - certificate has subject CN matching provided hostname
func verifyCertificate(ctx context.Context, secretName, namespace, hostname string) error {
	return wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
		secret, err := loader.KubeClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		isVerified, err := library.VerifyCertificate(secret, hostname)
		if err != nil {
			return false, err
		}
		return isVerified, nil
	})
}

// verifyCertificateRenewed repeatedly loads the tls secret as a X509 certificate every pollDuration
// and returns no error if certificate was renewed at least once
func verifyCertificateRenewed(ctx context.Context, secretName, namespace string, pollDuration time.Duration) error {
	var initExpiryTime *time.Time
	return wait.PollImmediate(pollDuration, TestTimeout, func() (bool, error) {
		secret, err := loader.KubeClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		cert, err := library.GetX509Certificate(secret)
		if err != nil {
			return false, err
		}

		// cert expiry time is recorded upon initial run
		if initExpiryTime == nil {
			initExpiryTime = &cert.NotAfter
		}

		// checks if expiry time was updated
		if *initExpiryTime == cert.NotAfter {
			return false, nil
		}

		// iff expiry time was updated, check if new expiry is not ahead
		// return an error, else certificate was renewed properly
		if !cert.NotAfter.After(*initExpiryTime) {
			return false, fmt.Errorf("previous expiry time of the certificate cannot be ahead of the current expiry time")
		}

		// certificate was renewed atleast once
		return true, nil
	})
}

// create randomized string
func randomStr(size int) string {
	char := "abcdefghijklmnopqrstuvwxyz0123456789"
	rand.NewSource(time.Now().UnixNano())
	var s bytes.Buffer
	for i := 0; i < size; i++ {
		s.WriteByte(char[rand.Int63()%int64(len(char))])
	}
	return s.String()
}

// replace string in bytes of ReadFile
func replaceStrInFile(replaceStrMap map[string]string, fileName string) ([]byte, error) {
	bytes, err := testassets.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	fileContentsStr := string(bytes)
	for k, v := range replaceStrMap {
		fileContentsStr = strings.ReplaceAll(fileContentsStr, k, v)
	}
	return []byte(fileContentsStr), nil
}

// waitForCertificateReadinessWithClient polls the status of the Certificate object and returns non-nil error
// once the Ready condition is true, otherwise should return a time-out error.
func waitForCertificateReadinessWithClient(ctx context.Context, client *certmanagerclientset.Clientset, certName, namespace string) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		cert, err := client.CertmanagerV1().Certificates(namespace).Get(ctx, certName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		for _, cond := range cert.Status.Conditions {
			if cond.Type == cmv1.CertificateConditionReady {
				return cond.Status == cmmetav1.ConditionTrue, nil
			}
		}
		return false, nil
	})
}

// waitForIngressReadiness polls the status of the Ingress object and returns non-nil error
// once the ingress endpoint is available, otherwise should return a time-out error.
func waitForIngressReadiness(ctx context.Context, client kubernetes.Interface, ingressObj networkingv1.Ingress, domainName string) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		ingress, err := client.NetworkingV1().Ingresses(ingressObj.Namespace).Get(ctx, ingressObj.Name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		endpoints := ingress.Status.LoadBalancer.Ingress
		if endpoints == nil {
			return false, nil
		}
		for _, endpoint := range endpoints {
			matched, err := regexp.MatchString(fmt.Sprintf(".%s$", domainName), endpoint.Hostname)
			if err != nil {
				return false, nil
			}
			return matched, nil
		}
		return false, nil
	})
}
