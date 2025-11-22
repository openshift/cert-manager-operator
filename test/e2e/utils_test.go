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
	"strings"
	"sync"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	opv1 "github.com/openshift/api/operator/v1"
	"github.com/tidwall/gjson"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	"github.com/openshift/cert-manager-operator/test/library"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
)

var (

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

var subscriptionSchema = schema.GroupVersionResource{
	Group:    "operators.coreos.com",
	Version:  "v1alpha1",
	Resource: "subscriptions",
}

var istiocsrSchema = schema.GroupVersionResource{
	Group:    "operator.openshift.io",
	Version:  "v1alpha1",
	Resource: "istiocsrs",
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
			cfg := updatedOperator.Spec.ControllerConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideArgs = args
			updatedOperator.Spec.ControllerConfig = cfg
		case certmanagerWebhookDeployment:
			cfg := updatedOperator.Spec.WebhookConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideArgs = args
			updatedOperator.Spec.WebhookConfig = cfg
		case certmanagerCAinjectorDeployment:
			cfg := updatedOperator.Spec.CAInjectorConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideArgs = args
			updatedOperator.Spec.CAInjectorConfig = cfg
		default:
			return fmt.Errorf("unsupported deployment name: %s", deploymentName)
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentArgs polls every $fastPollInterval to check if the deployment args list is updated to contain the
// passed args. It returns an error if a timeout ($lowTimeout) occurs or an error was encountered while polling
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
			cfg := updatedOperator.Spec.ControllerConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideEnv = env
			updatedOperator.Spec.ControllerConfig = cfg
		case certmanagerWebhookDeployment:
			cfg := updatedOperator.Spec.WebhookConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideEnv = env
			updatedOperator.Spec.WebhookConfig = cfg
		case certmanagerCAinjectorDeployment:
			cfg := updatedOperator.Spec.CAInjectorConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideEnv = env
			updatedOperator.Spec.CAInjectorConfig = cfg
		default:
			return fmt.Errorf("unsupported deployment name: %s", deploymentName)
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentEnv polls every $fastPollInterval to check if the deployment env list is updated to contain the
// passed env. It returns an error if a timeout ($lowTimeout) occurs or an error was encountered while polling
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
			cfg := updatedOperator.Spec.ControllerConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideResources = res
			updatedOperator.Spec.ControllerConfig = cfg
		case certmanagerWebhookDeployment:
			cfg := updatedOperator.Spec.WebhookConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideResources = res
			updatedOperator.Spec.WebhookConfig = cfg
		case certmanagerCAinjectorDeployment:
			cfg := updatedOperator.Spec.CAInjectorConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideResources = res
			updatedOperator.Spec.CAInjectorConfig = cfg
		default:
			return fmt.Errorf("unsupported deployment name: %s", deploymentName)
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentResources polls every $slowPollInterval to check if the deployment resources is updated to contain
// the passed resources. It returns an error if a timeout ($highTimeout) occurs or an error was encountered while
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
			cfg := updatedOperator.Spec.ControllerConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideScheduling = res
			updatedOperator.Spec.ControllerConfig = cfg
		case certmanagerWebhookDeployment:
			cfg := updatedOperator.Spec.WebhookConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideScheduling = res
			updatedOperator.Spec.WebhookConfig = cfg
		case certmanagerCAinjectorDeployment:
			cfg := updatedOperator.Spec.CAInjectorConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideScheduling = res
			updatedOperator.Spec.CAInjectorConfig = cfg
		default:
			return fmt.Errorf("unsupported deployment name: %s", deploymentName)
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentScheduling polls every $slowPollInterval to check if the deployment scheduling is updated to contain
// the passed scheduling. It returns an error if a timeout ($highTimeout) occurs or an error was encountered while
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

// addOverrideReplicas adds the override replicas to the specific cert-manager operand. The update process
// is retried if a conflict error is encountered.
func addOverrideReplicas(client *certmanoperatorclient.Clientset, deploymentName string, replicas *int32) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}

		updatedOperator := operator.DeepCopy()

		switch deploymentName {
		case certmanagerControllerDeployment:
			cfg := updatedOperator.Spec.ControllerConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideReplicas = replicas
			updatedOperator.Spec.ControllerConfig = cfg
		case certmanagerWebhookDeployment:
			cfg := updatedOperator.Spec.WebhookConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideReplicas = replicas
			updatedOperator.Spec.WebhookConfig = cfg
		case certmanagerCAinjectorDeployment:
			cfg := updatedOperator.Spec.CAInjectorConfig
			if cfg == nil {
				cfg = &v1alpha1.DeploymentConfig{}
			}
			cfg.OverrideReplicas = replicas
			updatedOperator.Spec.CAInjectorConfig = cfg
		default:
			return fmt.Errorf("unsupported deployment name: %s", deploymentName)
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentReplicas polls every $fastPollInterval to check if the deployment replicas is updated to contain
// the passed replica count. It returns an error if a timeout ($lowTimeout) occurs or an error was encountered while
// polling the deployment replicas.
func verifyDeploymentReplicas(k8sclient *kubernetes.Clientset, deploymentName string, expectedReplicas *int32, shouldMatch bool) error {

	return wait.PollUntilContextTimeout(context.TODO(), fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		deployment, err := k8sclient.AppsV1().Deployments(operandNamespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		// Get expected replica count - defaults to 1 when nil
		expectedReplicaCount := int32(1)
		if expectedReplicas != nil {
			expectedReplicaCount = *expectedReplicas
		}

		// Get actual replica count from deployment spec - defaults to 1 when nil
		actualReplicaCount := int32(1)
		if deployment.Spec.Replicas != nil {
			actualReplicaCount = *deployment.Spec.Replicas
		}

		// Check if spec replica count matches expected
		replicasMatch := actualReplicaCount == expectedReplicaCount

		if shouldMatch {
			return replicasMatch, nil
		}
		return !replicasMatch, nil
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

// patchSubscriptionWithEnvVars uses the k8s dynamic client to patch the only Subscription object
// in the cert-manager-operator namespace, inject specified env vars into spec.config.env
func patchSubscriptionWithEnvVars(ctx context.Context, loader library.DynamicResourceLoader, envVars map[string]string) error {
	subName, err := getCertManagerOperatorSubscription(ctx, loader)
	if err != nil {
		return err
	}

	env := make([]interface{}, len(envVars))
	i := 0
	for k, v := range envVars {
		env[i] = map[string]interface{}{
			"name":  k,
			"value": v,
		}
		i++
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"config": map[string]interface{}{
				"env": env,
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

// waitForDeploymentRollout waits for a deployment to complete its rollout
// This checks that the deployment's observed generation matches its generation
// and that all replicas are available
func waitForDeploymentRollout(ctx context.Context, namespace, deploymentName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		deployment, err := k8sClientSet.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		// Default desired replicas to 1 when unset, as elsewhere in this file.
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}

		// Check if rollout is complete
		if deployment.Generation != deployment.Status.ObservedGeneration {
			return false, nil
		}
		if deployment.Status.UpdatedReplicas < desired {
			return false, nil
		}
		if deployment.Status.AvailableReplicas < desired {
			return false, nil
		}

		return true, nil
	})
}

// waitForClusterIssuerReadiness waits for a ClusterIssuer to become Ready
func waitForClusterIssuerReadiness(ctx context.Context, clusterIssuerName string) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true,
		func(context.Context) (bool, error) {
			clusterIss, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Get(ctx, clusterIssuerName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			for _, cond := range clusterIss.Status.Conditions {
				if cond.Type == cmv1.IssuerConditionReady {
					return cond.Status == cmmetav1.ConditionTrue, nil
				}
			}
			return false, nil
		},
	)
}

// waitForIssuerReadiness waits for an Issuer to become Ready
func waitForIssuerReadiness(ctx context.Context, issuerName, namespace string) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true,
		func(context.Context) (bool, error) {
			iss, err := certmanagerClient.CertmanagerV1().Issuers(namespace).Get(ctx, issuerName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			for _, cond := range iss.Status.Conditions {
				if cond.Type == cmv1.IssuerConditionReady {
					return cond.Status == cmmetav1.ConditionTrue, nil
				}
			}
			return false, nil
		},
	)
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
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		return fmt.Errorf("timeout waiting for deployment %s/%s: ready %d/%d, updated %d/%d",
			namespace, deploymentName,
			deployment.Status.ReadyReplicas, desired,
			deployment.Status.UpdatedReplicas, desired)
	}

	return err
}

// pollTillConfigMapAvailable poll the configmap object and returns non-nil error
// once the configmap is available, otherwise should return a time-out error
func pollTillConfigMapAvailable(ctx context.Context, clientset *kubernetes.Clientset, namespace, configMapName string) error {
	err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		_, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
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

// pollTillConfigMapRemains poll to ensure a configmap does NOT exist for a specified duration
// Returns nil if the configmap consistently does not exist, error if it gets created
func pollTillConfigMapRemains(ctx context.Context, clientset *kubernetes.Clientset, namespace, configMapName string, duration time.Duration) error {
	err := wait.PollUntilContextTimeout(ctx, fastPollInterval, duration, true, func(context.Context) (bool, error) {
		_, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// ConfigMap doesn't exist, which is what we want
				return false, nil // Continue polling to ensure it stays absent
			}
			return false, err // Some other error occurred
		}

		// ConfigMap exists when it shouldn't
		return false, fmt.Errorf("configmap %s/%s should not exist but was found", namespace, configMapName)
	})

	// If we timed out without the ConfigMap being created, that's success
	if wait.Interrupted(err) {
		return nil
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

// resetCertManagerNetworkPolicyState resets the CertManager to have defaultNetworkPolicy="true"
func resetCertManagerNetworkPolicyState(ctx context.Context, client *certmanoperatorclient.Clientset, loader library.DynamicResourceLoader) error {
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

		// Set defaultNetworkPolicy to "true" to enable default network policies
		updatedOperator.Spec.DefaultNetworkPolicy = "true"

		// Clear custom network policies to start with only default ones
		updatedOperator.Spec.NetworkPolicies = []v1alpha1.NetworkPolicy{
			{
				Name:          "allow-egress-to-acme-server",
				ComponentName: "CoreController",
				Egress: []networkingv1.NetworkPolicyEgressRule{
					{
						Ports: []networkingv1.NetworkPolicyPort{
							{
								Protocol: ptr.To(corev1.ProtocolTCP),
								Port:     ptr.To(intstr.FromInt32(80)),
							},
							{
								Protocol: ptr.To(corev1.ProtocolTCP),
								Port:     ptr.To(intstr.FromInt32(443)),
							},
						},
					},
				},
			},
			{
				Name:          "allow-egress-to-dns-service",
				ComponentName: "CoreController",
				Egress: []networkingv1.NetworkPolicyEgressRule{
					{
						Ports: []networkingv1.NetworkPolicyPort{
							{
								Protocol: ptr.To(corev1.ProtocolTCP),
								Port:     ptr.To(intstr.FromInt32(53)),
							},
							{
								Protocol: ptr.To(corev1.ProtocolUDP),
								Port:     ptr.To(intstr.FromInt32(53)),
							},
						},
					},
				},
			},
			{
				Name:          "allow-egress-to-proxy",
				ComponentName: "CoreController",
				Egress: []networkingv1.NetworkPolicyEgressRule{
					{
						Ports: []networkingv1.NetworkPolicyPort{
							{
								Protocol: ptr.To(corev1.ProtocolTCP),
								Port:     ptr.To(intstr.FromInt32(3128)),
							},
						},
					},
				},
			},
			{
				Name:          "allow-egress-to-vault",
				ComponentName: "CoreController",
				Egress: []networkingv1.NetworkPolicyEgressRule{
					{
						Ports: []networkingv1.NetworkPolicyPort{
							{
								Protocol: ptr.To(corev1.ProtocolTCP),
								Port:     ptr.To(intstr.FromInt32(8200)),
							},
						},
					},
				},
			},
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return err
	}

	return nil
}

// execInPod executes a command in a specific container of a pod and returns the output.
func execInPod(ctx context.Context, cfg *rest.Config, kubeClient kubernetes.Interface, namespace, podName, containerName string, command ...string) (string, error) {
	req := kubeClient.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", fmt.Errorf("failed to execute command: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// createCertificateForVaultServer creates a self-signed certificate for Vault HTTPS server.
func createCertificateForVaultServer(ctx context.Context, certmanagerClient *certmanagerclientset.Clientset, namespace, vaultServiceName string) error {
	// Create self-signed ClusterIssuer for Vault
	clusterIssuerName := "vault-selfsigned-issuer"
	clusterIssuer := &cmv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterIssuerName,
		},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				SelfSigned: &cmv1.SelfSignedIssuer{},
			},
		},
	}
	_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create ClusterIssuer: %w", err)
	}

	// Wait for ClusterIssuer to become ready
	err = wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true,
		func(context.Context) (bool, error) {
			issuer, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Get(ctx, clusterIssuerName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			for _, cond := range issuer.Status.Conditions {
				if cond.Type == cmv1.IssuerConditionReady {
					return cond.Status == cmmetav1.ConditionTrue, nil
				}
			}
			return false, nil
		},
	)
	if err != nil {
		return fmt.Errorf("ClusterIssuer not ready: %w", err)
	}

	// Create certificate for Vault server
	certName := "vault-server-cert"
	cert := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: namespace,
		},
		Spec: cmv1.CertificateSpec{
			SecretName: "vault-server-tls",
			CommonName: vaultServiceName,
			DNSNames: []string{
				"vault",
				fmt.Sprintf("%s.%s.svc", vaultServiceName, namespace),
				fmt.Sprintf("%s.%s.svc.cluster.local", vaultServiceName, namespace),
			},
			IPAddresses: []string{
				"127.0.0.1",
			},
			IssuerRef: cmmetav1.ObjectReference{
				Name: clusterIssuerName,
				Kind: "ClusterIssuer",
			},
		},
	}
	_, err = certmanagerClient.CertmanagerV1().Certificates(namespace).Create(ctx, cert, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create Certificate: %w", err)
	}

	// Wait for certificate to become ready
	err = wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true,
		func(context.Context) (bool, error) {
			certificate, err := certmanagerClient.CertmanagerV1().Certificates(namespace).Get(ctx, certName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			for _, cond := range certificate.Status.Conditions {
				if cond.Type == cmv1.CertificateConditionReady {
					return cond.Status == cmmetav1.ConditionTrue, nil
				}
			}
			return false, nil
		},
	)
	if err != nil {
		return fmt.Errorf("Certificate not ready: %w", err)
	}

	return nil
}

// configureVaultPKI configures Vault PKI secrets engine for certificate issuance.
func configureVaultPKI(ctx context.Context, cfg *rest.Config, loader library.DynamicResourceLoader, namespace, vaultPodName, rootToken string) error {
	kubeClient := loader.KubeClient

	// Set VAULT_TOKEN environment variable for subsequent commands
	tokenEnv := fmt.Sprintf("export VAULT_TOKEN=%s", rootToken)

	// Enable and configure root PKI engine
	commands := []struct {
		description string
		cmd         string
	}{
		{
			"enable PKI secrets engine",
			tokenEnv + " && vault secrets enable pki",
		},
		{
			"tune PKI max lease TTL",
			tokenEnv + " && vault secrets tune -max-lease-ttl=8760h pki",
		},
		{
			"generate root CA",
			tokenEnv + " && vault write pki/root/generate/internal common_name=cluster.local ttl=8760h",
		},
		{
			"configure CA and CRL URLs",
			tokenEnv + " && vault write pki/config/urls issuing_certificates=\"https://vault:8200/v1/pki/ca\" crl_distribution_points=\"https://vault:8200/v1/pki/crl\"",
		},
		{
			"enable intermediate PKI",
			tokenEnv + " && vault secrets enable -path=pki_int pki",
		},
		{
			"tune intermediate PKI max lease TTL",
			tokenEnv + " && vault secrets tune -max-lease-ttl=4380h pki_int",
		},
	}

	for _, cmdInfo := range commands {
		_, err := execInPod(ctx, cfg, kubeClient, namespace, vaultPodName, "vault", "sh", "-c", cmdInfo.cmd)
		if err != nil {
			return fmt.Errorf("failed to %s: %w", cmdInfo.description, err)
		}
	}

	// Generate intermediate CSR
	csrOutput, err := execInPod(ctx, cfg, kubeClient, namespace, vaultPodName, "vault", "sh", "-c",
		tokenEnv+` && vault write -format=json pki_int/intermediate/generate/internal common_name="cluster.local Intermediate Authority" ttl=4380h`)
	if err != nil {
		return fmt.Errorf("failed to generate intermediate CSR: %w", err)
	}
	csr := gjson.Get(csrOutput, "data.csr").String()
	if csr == "" {
		return fmt.Errorf("failed to extract CSR from output")
	}

	// Sign intermediate with root CA - use heredoc to properly handle multi-line CSR
	signCmd := tokenEnv + ` && vault write -format=json pki/root/sign-intermediate format=pem_bundle ttl=4380h csr=- <<EOF
` + csr + `
EOF`
	certOutput, err := execInPod(ctx, cfg, kubeClient, namespace, vaultPodName, "vault", "sh", "-c", signCmd)
	if err != nil {
		return fmt.Errorf("failed to sign intermediate certificate: %w", err)
	}
	signedCert := gjson.Get(certOutput, "data.certificate").String()
	if signedCert == "" {
		return fmt.Errorf("failed to extract signed certificate from output")
	}

	// Set signed certificate - use heredoc to properly handle multi-line certificate
	setSignedCmd := tokenEnv + ` && vault write pki_int/intermediate/set-signed certificate=- <<EOF
` + signedCert + `
EOF`
	_, err = execInPod(ctx, cfg, kubeClient, namespace, vaultPodName, "vault", "sh", "-c", setSignedCmd)
	if err != nil {
		return fmt.Errorf("failed to set signed intermediate certificate: %w", err)
	}

	// Create role for cert-manager
	_, err = execInPod(ctx, cfg, kubeClient, namespace, vaultPodName, "vault", "sh", "-c",
		tokenEnv+` && vault write pki_int/roles/cluster-dot-local allowed_domains=cluster.local allow_subdomains=true max_ttl=72h`)
	if err != nil {
		return fmt.Errorf("failed to create PKI role: %w", err)
	}

	// Create policy for cert-manager
	policyCmd := tokenEnv + ` && vault policy write cert-manager - <<EOF
path "pki_int/sign/cluster-dot-local" {
  capabilities = ["create", "update"]
}
path "pki_int/issue/cluster-dot-local" {
  capabilities = ["create", "update"]
}
EOF`
	_, err = execInPod(ctx, cfg, kubeClient, namespace, vaultPodName, "vault", "sh", "-c", policyCmd)
	if err != nil {
		return fmt.Errorf("failed to create cert-manager policy: %w", err)
	}

	log.Printf("Vault PKI configuration completed successfully")
	return nil
}

// setupVaultServer deploys and initializes a Vault server using Helm.
// This is more maintainable than custom StatefulSet logic.
// It returns the pod name, root token, ClusterRoleBinding name, and any error encountered.
// The caller is responsible for cleaning up the ClusterRoleBinding using the returned name.
func setupVaultServer(ctx context.Context, cfg *rest.Config, loader library.DynamicResourceLoader, certmanagerClient *certmanagerclientset.Clientset, namespace, releaseName string) (string, string, string, error) {
	kubeClient := loader.KubeClient
	vaultPodLabel := "app.kubernetes.io/name=vault"

	log.Printf("Creating TLS certificate for Vault server")
	err := createCertificateForVaultServer(ctx, certmanagerClient, namespace, releaseName)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to create Vault TLS certificate: %w", err)
	}

	// Load Helm values from embedded file
	helmValuesBytes, err := testassets.ReadFile("testdata/vault/helm-values.yaml")
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read Vault Helm values: %w", err)
	}
	helmValues := string(helmValuesBytes)

	// Create ConfigMap with Helm values
	helmConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vault-helm-config",
			Namespace: namespace,
		},
		Data: map[string]string{
			"custom-values.yaml": helmValues,
		},
	}
	_, err = kubeClient.CoreV1().ConfigMaps(namespace).Create(ctx, helmConfigMap, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return "", "", "", fmt.Errorf("failed to create Helm config ConfigMap for Vault %s in namespace %s: %w", releaseName, namespace, err)
	}

	// Create ServiceAccount for Helm installer
	serviceAccountName := "vault-installer-sa"
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: namespace,
		},
	}
	_, err = kubeClient.CoreV1().ServiceAccounts(namespace).Create(ctx, serviceAccount, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return "", "", "", fmt.Errorf("failed to create ServiceAccount: %w", err)
	}

	// Create ClusterRoleBinding for Helm installer
	clusterRoleBindingName := fmt.Sprintf("vault-installer-binding-%s", namespace)
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterRoleBindingName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: namespace,
			},
		},
	}
	_, err = kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, clusterRoleBinding, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return "", "", "", fmt.Errorf("failed to create ClusterRoleBinding: %w", err)
	}

	// Create Helm installer pod
	helmCmd := fmt.Sprintf("helm install %s ./vault -n %s --values /helm/custom-values.yaml", releaseName, namespace)

	privileged := true
	installerPodName := "vault-installer"
	helmPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      installerPodName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: serviceAccountName,
			RestartPolicy:      corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:            "helm",
					Image:           "quay.io/openshifttest/helm:3.17.0",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"sh", "-c", helmCmd},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "values-volume",
							MountPath: "/helm",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privileged,
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "values-volume",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "vault-helm-config",
							},
						},
					},
				},
			},
		},
	}
	_, err = kubeClient.CoreV1().Pods(namespace).Create(ctx, helmPod, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return "", "", "", fmt.Errorf("failed to create Helm installer pod: %w", err)
	}

	// Wait for Helm installer pod to complete
	log.Printf("Waiting for Helm installer pod to complete...")
	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 5*time.Minute, true,
		func(context.Context) (bool, error) {
			pod, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, installerPodName, metav1.GetOptions{})
			if err != nil {
				return false, nil
			}
			if pod.Status.Phase == corev1.PodSucceeded {
				log.Printf("Helm installer pod completed successfully")
				return true, nil
			}
			if pod.Status.Phase == corev1.PodFailed {
				// Get logs for debugging
				logs, _ := kubeClient.CoreV1().Pods(namespace).GetLogs(installerPodName, &corev1.PodLogOptions{TailLines: ptr.To(int64(20))}).DoRaw(ctx)
				return false, fmt.Errorf("Helm installer pod failed: %s", string(logs))
			}
			return false, nil
		},
	)
	if err != nil {
		// Try to get logs for debugging
		logs, _ := kubeClient.CoreV1().Pods(namespace).GetLogs(installerPodName, &corev1.PodLogOptions{TailLines: ptr.To(int64(50))}).DoRaw(ctx)
		return "", "", "", fmt.Errorf("timeout waiting for Helm installer: %w, logs: %s", err, string(logs))
	}

	// Wait for Vault pod to be running
	log.Printf("Waiting for Vault pod to start...")
	var vaultPodName string
	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 3*time.Minute, true,
		func(context.Context) (bool, error) {
			pods, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: vaultPodLabel,
			})
			if err != nil {
				return false, nil
			}
			if len(pods.Items) == 0 {
				return false, nil
			}
			pod := pods.Items[0]
			vaultPodName = pod.Name
			if pod.Status.Phase == corev1.PodRunning {
				// Check if container is running (not ready, since Vault needs to be unsealed)
				for _, cs := range pod.Status.ContainerStatuses {
					if cs.Name == "vault" && cs.State.Running != nil {
						log.Printf("Vault pod %s is running", vaultPodName)
						return true, nil
					}
				}
			}
			return false, nil
		},
	)
	if err != nil {
		return "", "", "", fmt.Errorf("timeout waiting for Vault pod to start: %w", err)
	}

	// Initialize and unseal Vault
	log.Printf("Initializing Vault...")
	initOutput, err := execInPod(ctx, cfg, kubeClient, namespace, vaultPodName, "vault", "vault", "operator", "init", "-key-shares=1", "-key-threshold=1", "-format=json")
	if err != nil {
		return "", "", "", fmt.Errorf("failed to initialize Vault: %w", err)
	}

	unsealKey := gjson.Get(initOutput, "unseal_keys_b64.0").String()
	rootToken := gjson.Get(initOutput, "root_token").String()
	if unsealKey == "" || rootToken == "" {
		return "", "", "", fmt.Errorf("failed to extract unseal key or root token from init output")
	}

	log.Printf("Unsealing Vault...")
	_, err = execInPod(ctx, cfg, kubeClient, namespace, vaultPodName, "vault", "vault", "operator", "unseal", unsealKey)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to unseal Vault: %w", err)
	}

	// Wait for Vault to become ready (sealed=false)
	log.Printf("Waiting for Vault to become ready...")
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true,
		func(context.Context) (bool, error) {
			output, err := execInPod(ctx, cfg, kubeClient, namespace, vaultPodName, "vault", "vault", "status", "-format=json")
			if err != nil {
				// vault status returns non-zero when sealed, so we check the output
				if strings.Contains(output, `"sealed":false`) {
					return true, nil
				}
				return false, nil
			}
			sealed := gjson.Get(output, "sealed").String()
			return sealed == "false", nil
		},
	)
	if err != nil {
		return "", "", "", fmt.Errorf("timeout waiting for Vault to become unsealed: %w", err)
	}

	log.Printf("Vault server setup completed successfully")
	return vaultPodName, rootToken, clusterRoleBindingName, nil
}
