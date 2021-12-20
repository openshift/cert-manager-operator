package deployment

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func Test_deploymentChecker_isAnyDeploymentPresent(t *testing.T) {
	type args struct {
		testDeployments map[string][]string
	}
	tests := []struct {
		args
		name                 string
		deploymentsInCluster map[string][]string
		wantResult           bool
	}{
		{
			name: "Test existing deployments",
			args: args{
				testDeployments: map[string][]string{
					"test": {"test"},
				},
			},
			deploymentsInCluster: map[string][]string{
				"test": {"test"},
			},
			wantResult: true,
		},
		{
			name: "Test missing deployments",
			args: args{
				testDeployments: map[string][]string{
					"test": {"test"},
				},
			},
			deploymentsInCluster: map[string][]string{},
			wantResult:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &deploymentChecker{}
			d.deploymentsGetterFunc = deploymentsGetterMock
			existingDeploymentsInCluster = tt.deploymentsInCluster

			got, _ := d.isAnyDeploymentPresent(context.TODO(), tt.args.testDeployments)
			if got != tt.wantResult {
				t.Errorf("isAnyDeploymentPresent() got = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

var existingDeploymentsInCluster map[string][]string

func deploymentsGetterMock(_ context.Context, namespace, deploymentName string) error {
	for n, d := range existingDeploymentsInCluster {
		if n == namespace {
			for _, dd := range d {
				if dd == deploymentName {
					return nil
				}
			}
		}
	}
	return errors.NewNotFound(schema.GroupResource{}, deploymentName)
}
