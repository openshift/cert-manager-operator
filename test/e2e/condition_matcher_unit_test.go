//go:build e2e
// +build e2e

package e2e

import (
	"strings"
	"testing"

	opv1 "github.com/openshift/api/operator/v1"
	operatorv1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	fakecertmanclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestVerifyOperatorStatusCondition unit test for verifyOperatorStatusCondition func
func TestVerifyOperatorStatusCondition(t *testing.T) {
	controllerPrefix := "foo-controller"

	newCertManagerObjectWithConditions := func(conditions ...opv1.OperatorCondition) *operatorv1alpha1.CertManager {
		return &operatorv1alpha1.CertManager{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Status: operatorv1alpha1.CertManagerStatus{
				OperatorStatus: opv1.OperatorStatus{
					Conditions: conditions,
				},
			},
		}
	}

	tests := []struct {
		name               string
		initialObjects     []runtime.Object
		expectedConditions map[string]opv1.ConditionStatus
		matchAny           bool
		expectError        bool
		errorContains      string
	}{
		{
			name: "One degraded is true but another is false using Any",
			expectedConditions: map[string]opv1.ConditionStatus{
				"Available":   opv1.ConditionTrue,
				"Degraded":    opv1.ConditionFalse,
				"Progressing": opv1.ConditionFalse,
			},
			initialObjects: []runtime.Object{
				newCertManagerObjectWithConditions(
					opv1.OperatorCondition{Type: controllerPrefix + "Available", Status: opv1.ConditionTrue},

					opv1.OperatorCondition{Type: controllerPrefix + "Degraded", Status: opv1.ConditionFalse},
					opv1.OperatorCondition{Type: controllerPrefix + "-rand-Degraded", Status: opv1.ConditionTrue},

					opv1.OperatorCondition{Type: controllerPrefix + "Progressing", Status: opv1.ConditionFalse},
				),
			},
			matchAny:      true,
			expectError:   false,
			errorContains: "context deadline exceeded",
		},
		{
			name: "Both degraded is false",
			expectedConditions: map[string]opv1.ConditionStatus{
				"Available":   opv1.ConditionTrue,
				"Degraded":    opv1.ConditionFalse,
				"Progressing": opv1.ConditionFalse,
			},
			initialObjects: []runtime.Object{
				newCertManagerObjectWithConditions(
					opv1.OperatorCondition{Type: controllerPrefix + "Available", Status: opv1.ConditionTrue},

					opv1.OperatorCondition{Type: controllerPrefix + "Degraded", Status: opv1.ConditionFalse},
					opv1.OperatorCondition{Type: controllerPrefix + "-bar-Degraded", Status: opv1.ConditionFalse},

					opv1.OperatorCondition{Type: controllerPrefix + "Progressing", Status: opv1.ConditionFalse},
				),
			},
			expectError: false,
		},
		{
			name: "One available is true but another is false using All",
			expectedConditions: map[string]opv1.ConditionStatus{
				"Available":   opv1.ConditionTrue,
				"Degraded":    opv1.ConditionFalse,
				"Progressing": opv1.ConditionFalse,
			},
			initialObjects: []runtime.Object{
				newCertManagerObjectWithConditions(
					opv1.OperatorCondition{Type: controllerPrefix + "Available", Status: opv1.ConditionTrue},
					opv1.OperatorCondition{Type: controllerPrefix + "-bar-Available", Status: opv1.ConditionFalse},

					opv1.OperatorCondition{Type: controllerPrefix + "Degraded", Status: opv1.ConditionFalse},
					opv1.OperatorCondition{Type: controllerPrefix + "-bar-Degraded", Status: opv1.ConditionFalse},

					opv1.OperatorCondition{Type: controllerPrefix + "Progressing", Status: opv1.ConditionFalse},
				),
			},
			expectError:   true,
			errorContains: "context deadline exceeded",
		},
		{
			name: "A missing condition for Progressing",
			expectedConditions: map[string]opv1.ConditionStatus{
				"Available":   opv1.ConditionTrue,
				"Degraded":    opv1.ConditionFalse,
				"Progressing": opv1.ConditionFalse,
			},
			initialObjects: []runtime.Object{
				newCertManagerObjectWithConditions(
					opv1.OperatorCondition{Type: controllerPrefix + "Available", Status: opv1.ConditionTrue},
					opv1.OperatorCondition{Type: controllerPrefix + "-bar-Available", Status: opv1.ConditionFalse},

					opv1.OperatorCondition{Type: controllerPrefix + "Degraded", Status: opv1.ConditionFalse},
					opv1.OperatorCondition{Type: controllerPrefix + "-bar-Degraded", Status: opv1.ConditionFalse},
				),
			},
			expectError:   true,
			errorContains: "could not verify all status conditions as expected",
		},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fakecertmanclient.NewSimpleClientset(tt.initialObjects...)

			matchers := GenerateConditionMatchers([]PrefixAndMatchTypeTuple{
				{controllerPrefix, tt.matchAny},
			}, tt.expectedConditions)

			err := verifyOperatorStatusCondition(fakeClient.OperatorV1alpha1(), matchers)

			if tt.expectError && err == nil {
				t.Errorf("Expected an error, but got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, but got: %v", err)
			}

			if tt.expectError && err != nil {
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', but got: %v", tt.errorContains, err)
				}
			}
		})
	}
}
