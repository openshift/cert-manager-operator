package e2e

import (
	"context"
	"fmt"
	"sync"
	"time"

	opv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	certmanagerControllerDeployment = "cert-manager"
	certmanagerWebhookDeployment    = "cert-manager-webhook"
	certmanagerCAinjectorDeployment = "cert-manager-cainjector"
)

// waitForValidOperatorStatusCondition polls every 1 second to check if the status of each of the controllers
// has become available or not. It returns an error if a timeout (5 mins) occurs or an error was encountered
// which polling the status. For each controller a the polling happens in separate go-routines.
func waitForValidOperatorStatusCondition(client *certmanoperatorclient.Clientset, controllerNames []string) error {

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

				flag := false
				for _, cond := range operator.Status.Conditions {
					if cond.Type == controllerNames[index]+"Available" {
						flag = cond.Status == opv1.ConditionTrue
					}

					if cond.Type == controllerNames[index]+"Degraded" {
						flag = cond.Status == opv1.ConditionFalse
					}

					if cond.Type == controllerNames[index]+"Progressing" {
						flag = cond.Status == opv1.ConditionFalse
					}
				}

				return flag, nil
			})
			errs[index] = err
		}(index)
	}
	wg.Wait()

	return errors.NewAggregate(errs)
}

// waitForInvalidOperatorStatusCondition polls every 1 second to check if the status of each of the controllers
// has become degraded or not. It returns an error if a timeout (5 mins) occurs or an error was encountered
// which polling the status. For each controller a the polling happens in separate go-routines.
func waitForInvalidOperatorStatusCondition(client *certmanoperatorclient.Clientset, controllerNames []string) error {

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

				flag := false
				for _, cond := range operator.Status.Conditions {
					if cond.Type == controllerNames[index]+"Degraded" {
						flag = cond.Status == opv1.ConditionTrue
					}
				}

				return flag, nil
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

// waitForDeploymentArgs polls every 1 second to check if the deployment args list is updated to contain the
// passed args. It returns an error if a timeout (5 mins) occurs or an error was encountered while polling
// the deployment args list.
func waitForDeploymentArgs(k8sclient *kubernetes.Clientset, deploymentName string, args []string, added bool) error {

	return wait.PollImmediate(time.Second*1, time.Minute*5, func() (done bool, err error) {
		controllerDeployment, err := k8sclient.AppsV1().Deployments(operatorclient.TargetNamespace).Get(context.TODO(), deploymentName, v1.GetOptions{})
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
