//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	opv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	"github.com/openshift/cert-manager-operator/test/library"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	// cert-manager operator deployment details
	operatorNamespace      = "cert-manager-operator"
	operatorDeploymentName = "cert-manager-operator-controller-manager"
)

var (
	// initialOperatorEnvVars stores the original environment variables
	// from the cert-manager operator deployment to restore during reset
	initialOperatorEnvVars []corev1.EnvVar

	// slowPollInterval and highTimeout are generally
	// used together in poll(s) where slow reaction and
	// larger timeout is necessary.
	// eg. used in Certificate readiness

	slowPollInterval = 12 * time.Second
	highTimeout      = 10 * time.Minute

	// fastPollInterval and lowTimeout are
	// used together in poll(s) with fast reaction and
	// smaller timeout window.
	// eg. used in overrides test for Status.Conditions

	fastPollInterval = 2 * time.Second
	lowTimeout       = 3 * time.Minute
)

//go:embed testdata/*
var testassets embed.FS

// Deployment schema for cert-manager operator
var operatorDeploymentSchema = schema.GroupVersionResource{
	Group:    "apps",
	Version:  "v1",
	Resource: "deployments",
}

var istiocsrSchema = schema.GroupVersionResource{
	Group:    "operator.openshift.io",
	Version:  "v1alpha1",
	Resource: "istiocsrs",
}

// storeInitialOperatorEnvVars captures and stores the initial environment variables
// from the cert-manager operator deployment for later restoration
func storeInitialOperatorEnvVars(ctx context.Context, clientset *kubernetes.Clientset) error {
	deployment, err := clientset.AppsV1().Deployments(operatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get operator deployment: %w", err)
	}

	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("operator deployment has no containers")
	}

	// Store a deep copy of the initial environment variables
	container := deployment.Spec.Template.Spec.Containers[0]
	initialOperatorEnvVars = make([]corev1.EnvVar, len(container.Env))
	copy(initialOperatorEnvVars, container.Env)

	return nil
}

// resetOperatorDeploymentEnvVars resets the cert-manager operator deployment
// environment variables to their initial state
func resetOperatorDeploymentEnvVars(ctx context.Context, clientset kubernetes.Interface) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := clientset.AppsV1().Deployments(operatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get operator deployment: %w", err)
		}

		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			return fmt.Errorf("operator deployment has no containers")
		}

		// Reset environment variables to initial state
		deployment.Spec.Template.Spec.Containers[0].Env = make([]corev1.EnvVar, len(initialOperatorEnvVars))
		copy(deployment.Spec.Template.Spec.Containers[0].Env, initialOperatorEnvVars)

		_, err = clientset.AppsV1().Deployments(operatorNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
		return err
	})
}

func verifyDeploymentGenerationIsNotEmpty(client *certmanoperatorclient.Clientset, deployments []metav1.ObjectMeta) error {
	var wg sync.WaitGroup
	var lastFetchedGenerationStatus []opv1.GenerationStatus

	errs := make([]error, len(deployments))
	for index, deployMeta := range deployments {
		wg.Add(1)

		go func(idx int, nameAndNs *metav1.ObjectMeta) {
			defer wg.Done()

			err := wait.PollUntilContextTimeout(context.TODO(), fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
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

				lastFetchedGenerationStatus = operator.Status.DeepCopy().Generations

				var exists bool
				for _, gen := range operator.Status.Generations {
					// match deployment: name and namespace, group, resource
					if gen.Name != nameAndNs.Name || gen.Namespace != nameAndNs.Namespace ||
						gen.Group != "apps" || gen.Resource != "deployments" {
						continue
					}
					exists = true

					if gen.LastGeneration <= 0 {
						return false, nil
					}
				}

				return exists, nil
			})

			errs[idx] = err
		}(index, &deployMeta)
	}
	wg.Wait()

	if err := errors.NewAggregate(errs); err != nil {
		prettyGens, _ := json.Marshal(lastFetchedGenerationStatus)
		log.Printf("found status.generations: %s", prettyGens)

		return fmt.Errorf("could not verify deployment generation status : %v", err)
	}

	return nil
}

// resetCertManagerState is used to revert back to the default cert-manager operands' state
func resetCertManagerState(ctx context.Context, client *certmanoperatorclient.Clientset, loader library.DynamicResourceLoader) error {
	// update operator spec to empty *Config and set operatorSpec to default values
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var operatorState *v1alpha1.CertManager
		err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
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
	if err != nil {
		return err
	}

	// reset cert-manager operator deployment environment variables to initial state
	return resetOperatorDeploymentEnvVars(ctx, loader.KubeClient)
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

	return wait.PollUntilContextTimeout(context.TODO(), fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
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

		containerArgsSet := sets.New(controllerDeployment.Spec.Template.Spec.Containers[0].Args...)

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

// addOverrideEnv adds the override environment variables to specific the cert-manager operand. The update process
// is retried if a conflict error is encountered.
func addOverrideEnv(client *certmanoperatorclient.Clientset, deploymentName string, env []corev1.EnvVar) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}

		updatedOperator := operator.DeepCopy()

		switch deploymentName {
		case certmanagerControllerDeployment:
			updatedOperator.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{
				OverrideEnv: env,
			}
		case certmanagerWebhookDeployment:
			updatedOperator.Spec.WebhookConfig = &v1alpha1.DeploymentConfig{
				OverrideEnv: env,
			}
		case certmanagerCAinjectorDeployment:
			updatedOperator.Spec.CAInjectorConfig = &v1alpha1.DeploymentConfig{
				OverrideEnv: env,
			}
		default:
			return fmt.Errorf("unsupported deployment name: %s", deploymentName)
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentEnv polls every 1 second to check if the deployment env list is updated to contain the
// passed env. It returns an error if a timeout (5 mins) occurs or an error was encountered while polling
// the deployment env list.
func verifyDeploymentEnv(k8sclient *kubernetes.Clientset, deploymentName string, env []corev1.EnvVar, added bool) error {

	return wait.PollUntilContextTimeout(context.TODO(), fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
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

		containerEnvList := sets.New(controllerDeployment.Spec.Template.Spec.Containers[0].Env...)

		if added {
			if !containerEnvList.HasAll(env...) {
				return false, nil
			}
		} else {
			if containerEnvList.HasAll(env...) {
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

	return wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
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

	return wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
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

// patchOperatorDeploymentWithEnvVars patches the cert-manager operator deployment
// to inject specified environment variables
func patchOperatorDeploymentWithEnvVars(ctx context.Context, clientset kubernetes.Interface, envVars map[string]string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := clientset.AppsV1().Deployments(operatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get operator deployment: %w", err)
		}

		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			return fmt.Errorf("operator deployment has no containers")
		}

		// Get current environment variables
		currentEnv := deployment.Spec.Template.Spec.Containers[0].Env

		// Create a map of existing env vars for easy lookup
		envMap := make(map[string]corev1.EnvVar)
		for _, env := range currentEnv {
			envMap[env.Name] = env
		}

		// Add or update environment variables
		for name, value := range envVars {
			envMap[name] = corev1.EnvVar{
				Name:  name,
				Value: value,
			}
		}

		// Convert back to slice
		newEnv := make([]corev1.EnvVar, 0, len(envMap))
		for _, env := range envMap {
			newEnv = append(newEnv, env)
		}

		deployment.Spec.Template.Spec.Containers[0].Env = newEnv

		_, err = clientset.AppsV1().Deployments(operatorNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
		return err
	})
}

// waitForCertificateReadiness polls the status of the Certificate object and returns non-nil error
// once the Ready condition is true, otherwise should return a time-out error
func waitForCertificateReadiness(ctx context.Context, certName, namespace string) error {
	return wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
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
	return wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
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
	return wait.PollUntilContextTimeout(context.TODO(), pollDuration, highTimeout, true, func(context.Context) (bool, error) {
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

// pollTillJobCompleted poll the job object and returns non-nil error
// once the job is completed, otherwise should return a time-out error
func pollTillJobCompleted(ctx context.Context, clientset *kubernetes.Clientset, namespace, jobName string) error {
	err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		job, err := clientset.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})

		if err != nil {
			return false, err
		}

		for _, cond := range job.Status.Conditions {
			if cond.Type == batchv1.JobComplete {
				if cond.Status == corev1.ConditionTrue {
					return true, nil
				} else {
					return false, nil
				}
			}
		}

		return false, nil
	})
	return err
}

// pollTillJobFailed poll the job object and returns non-nil error
// if job succeeds or encounters other issues. Returns nil when job fails.
func pollTillJobFailed(ctx context.Context, clientset *kubernetes.Clientset, namespace, jobName string) error {
	return wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(ctx context.Context) (bool, error) {
		job, err := clientset.BatchV1().Jobs(namespace).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if job.Status.Succeeded > 0 {
			return false, fmt.Errorf("job %s succeeded when we expected it to fail", jobName)
		}

		if job.Status.Failed > 0 {
			return true, nil
		}

		return false, nil
	})
}

// pollTillServiceAccountAvailable poll the service account object and returns non-nil error
// once the service account is available, otherwise should return a time-out error
func pollTillServiceAccountAvailable(ctx context.Context, clientset *kubernetes.Clientset, namespace, serviceAccountName string) error {
	err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		_, err := clientset.CoreV1().ServiceAccounts(namespace).Get(ctx, serviceAccountName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		return true, nil
	})

	return err
}

// pollTillIstioCSRAvailable poll the istioCSR object and returns non-nil error and istioCSRStatus
// once the istiocsr is available, otherwise should return a time-out error
func pollTillIstioCSRAvailable(ctx context.Context, loader library.DynamicResourceLoader, namespace, istioCsrName string) (v1alpha1.IstioCSRStatus, error) {
	var istioCSRStatus v1alpha1.IstioCSRStatus
	istiocsrClient := loader.DynamicClient.Resource(istiocsrSchema).Namespace(namespace)
	err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		customResource, err := istiocsrClient.Get(ctx, istioCsrName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		status, found, err := unstructured.NestedMap(customResource.Object, "status")
		if err != nil {
			return false, fmt.Errorf("failed to extract status from IstioCSR: %w", err)
		}
		if !found {
			return false, nil
		}

		err = runtime.DefaultUnstructuredConverter.FromUnstructured(status, &istioCSRStatus)
		if err != nil {
			return false, fmt.Errorf("failed to convert status to IstioCSRStatus: %w", err)
		}

		// Check if required fields are populated
		if library.IsEmptyString(istioCSRStatus.IstioCSRGRPCEndpoint) || library.IsEmptyString(istioCSRStatus.ClusterRoleBinding) || library.IsEmptyString(istioCSRStatus.IstioCSRImage) || library.IsEmptyString(istioCSRStatus.ServiceAccount) {
			return false, nil
		}

		// Check ready condition
		readyCondition := meta.FindStatusCondition(istioCSRStatus.Conditions, v1alpha1.Ready)
		if readyCondition == nil {
			return false, nil
		}

		// Check for degraded condition
		degradedCondition := meta.FindStatusCondition(istioCSRStatus.Conditions, v1alpha1.Degraded)
		if degradedCondition != nil && degradedCondition.Status == metav1.ConditionTrue {
			return false, fmt.Errorf("IstioCSR is degraded: %s", degradedCondition.Message)
		}

		return readyCondition.Status == metav1.ConditionTrue, nil
	})

	return istioCSRStatus, err
}

// pollTillDeploymentAvailable poll the deployment object and returns non-nil error
// once the deployment is available, otherwise should return a time-out error
func pollTillDeploymentAvailable(ctx context.Context, clientSet *kubernetes.Clientset, namespace, deploymentName string) error {
	err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		deployment, err := clientSet.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		// Check for failure conditions first
		for _, cond := range deployment.Status.Conditions {
			if (cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse) ||
				(cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue) {
				return false, fmt.Errorf("deployment failed: %s - %s", cond.Reason, cond.Message)
			}
			if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}

		return false, nil
	})

	if err != nil && wait.Interrupted(err) {
		deployment, getErr := clientSet.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if getErr != nil {
			if apierrors.IsNotFound(getErr) {
				return fmt.Errorf("timeout waiting for deployment %s/%s: deployment does not exist", namespace, deploymentName)
			}
			return fmt.Errorf("timeout waiting for deployment %s/%s: failed to get status: %v", namespace, deploymentName, getErr)
		}

		// Deployment exists but not ready
		return fmt.Errorf("timeout waiting for deployment %s/%s: ready %d/%d, updated %d/%d",
			namespace, deploymentName,
			deployment.Status.ReadyReplicas, *deployment.Spec.Replicas,
			deployment.Status.UpdatedReplicas, *deployment.Spec.Replicas)
	}

	return err
}

// VerifyContainerResources verifies that a container in a pod has resources matching the expected configuration.
// If containerName is empty, it verifies the first container.
func VerifyContainerResources(pod corev1.Pod, containerName string, expectedResources corev1.ResourceRequirements) error {
	var targetContainer *corev1.Container

	if containerName == "" {
		// Default to first container when no specific container name is provided
		targetContainer = &pod.Spec.Containers[0]
	} else {
		// Find the specific container by name
		for i := range pod.Spec.Containers {
			if pod.Spec.Containers[i].Name == containerName {
				targetContainer = &pod.Spec.Containers[i]
				break
			}
		}
		if targetContainer == nil {
			return fmt.Errorf("container '%s' not found in pod '%s'", containerName, pod.Name)
		}
	}

	// Verify limits
	if expectedResources.Limits != nil {
		for resourceType, expectedValue := range expectedResources.Limits {
			if actualValue := targetContainer.Resources.Limits[resourceType]; !actualValue.Equal(expectedValue) {
				return fmt.Errorf("%s limit for container '%s' in pod '%s' is '%v' but expected '%v'", resourceType, targetContainer.Name, pod.Name, actualValue, expectedValue)
			}
		}
	}

	// Verify requests
	if expectedResources.Requests != nil {
		for resourceType, expectedValue := range expectedResources.Requests {
			if actualValue := targetContainer.Resources.Requests[resourceType]; !actualValue.Equal(expectedValue) {
				return fmt.Errorf("%s request for container '%s' in pod '%s' is '%v' but expected '%v'", resourceType, targetContainer.Name, pod.Name, actualValue, expectedValue)
			}
		}
	}
	return nil
}
