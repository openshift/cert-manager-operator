package deployment

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

var knownCertManagerDeployments = map[string][]string{
	//https://github.com/jetstack/cert-manager/tree/master/deploy/charts/cert-manager
	//https://github.com/IBM/ibm-cert-manager-operator/blob/master/deploy/olm-catalog/ibm-cert-manager-operator/3.17.0/ibm-cert-manager-operator.v3.17.0.clusterserviceversion.yaml
	"openshift-operators": {"ibm-cert-mana`ger-operator", "cert-manager"},
	//https://github.com/komish/certmanagerdeployment-operator/blob/main/controllers/componentry/constants.go
	"cert-manager": {"cert-manager"},
}

type deploymentsGetterFunc func(ctx context.Context, namespace, deploymentName string) error

type deploymentChecker struct {
	deploymentsGetter     v1.DeploymentsGetter
	deploymentsGetterFunc deploymentsGetterFunc
}

func newDeploymentChecker(deploymentsGetter v1.DeploymentsGetter) *deploymentChecker {
	ret := &deploymentChecker{
		deploymentsGetter: deploymentsGetter,
	}
	ret.deploymentsGetterFunc = ret.deploymentsGetterImpl
	return ret
}

func (d *deploymentChecker) isAnyDeploymentPresent(ctx context.Context, namespacesAndDeployments map[string][]string) (bool, error) {
	for namespace, deployments := range namespacesAndDeployments {
		for _, deployment := range deployments {
			ret, err := d.isDeploymentPresent(ctx, namespace, deployment)
			if err != nil {
				return false, err
			}
			if ret == true {
				return true, nil
			}
		}
	}
	return false, nil
}

func (d *deploymentChecker) isDeploymentPresent(ctx context.Context, namespace, deploymentName string) (bool, error) {
	err := d.deploymentsGetterFunc(ctx, namespace, deploymentName)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err == nil {
		return true, nil
	}
	return false, err
}

func (d *deploymentChecker) deploymentsGetterImpl(ctx context.Context, namespace, deploymentName string) error {
	_, err := d.deploymentsGetter.Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	return err
}

func (d *deploymentChecker) shouldSync(ctx context.Context) (bool, error) {
	deploymentsExist, err := d.isAnyDeploymentPresent(ctx, knownCertManagerDeployments)
	if err != nil {
		return false, fmt.Errorf("failed to check existing deployments: %w", err)
	}
	if deploymentsExist {
		return false, fmt.Errorf("backoff: one of the known Cert Manager Operators was found in the cluster: %v", knownCertManagerDeployments)
	}
	return true, nil
}
