//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	opv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// verifyOperatorStatusCondition polls every 1 second to check if the status of each of the controllers
// has become available or not. It returns an error if a timeout (5 mins) occurs or an error was encountered
// which polling the status. For each controller a the polling happens in separate go-routines.
func verifyOperatorStatusCondition(client *certmanoperatorclient.Clientset, controllerNames []string, expectedConditionMap map[string]opv1.ConditionStatus) error {

	var wg sync.WaitGroup
	errs := make([]error, len(controllerNames))
	for index := range controllerNames {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			err := wait.PollImmediate(time.Second*1, time.Minute*5, func() (done bool, err error) {
				operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", v1.GetOptions{})
				if err != nil {
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

// removeOverrides removes all the overrides from all the cert-manager operands. The update process is retried if
// a conflict error is encountered.
func removeOverrides(client *certmanoperatorclient.Clientset) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", v1.GetOptions{})
		if err != nil {
			return err
		}

		updatedOperator := operator.DeepCopy()

		hasOverride := false
		if updatedOperator.Spec.ControllerConfig != nil {
			updatedOperator.Spec.ControllerConfig = nil
			hasOverride = true
		}
		if updatedOperator.Spec.WebhookConfig != nil {
			updatedOperator.Spec.WebhookConfig = nil
			hasOverride = true
		}
		if updatedOperator.Spec.CAInjectorConfig != nil {
			updatedOperator.Spec.CAInjectorConfig = nil
			hasOverride = true
		}

		if !hasOverride {
			return nil
		}

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, v1.UpdateOptions{})
		return err
	})

}

// addOverrideArgs adds the override args to specific the cert-manager operand. The update process is retried if
// a conflict error is encountered.
func addOverrideArgs(client *certmanoperatorclient.Clientset, deploymentName string, args []string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		operator, err := client.OperatorV1alpha1().CertManagers().Get(context.TODO(), "cluster", v1.GetOptions{})
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

		_, err = client.OperatorV1alpha1().CertManagers().Update(context.TODO(), updatedOperator, v1.UpdateOptions{})
		return err
	})
}

// verifyDeploymentArgs polls every 1 second to check if the deployment args list is updated to contain the
// passed args. It returns an error if a timeout (5 mins) occurs or an error was encountered while polling
// the deployment args list.
func verifyDeploymentArgs(k8sclient *kubernetes.Clientset, deploymentName string, args []string, added bool) error {

	return wait.PollImmediate(time.Second*1, time.Minute*5, func() (done bool, err error) {
		controllerDeployment, err := k8sclient.AppsV1().Deployments(operandNamespace).Get(context.TODO(), deploymentName, v1.GetOptions{})
		if err != nil {
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
