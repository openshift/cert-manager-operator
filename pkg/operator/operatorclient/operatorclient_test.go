package operatorclient

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	applyoperatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testingclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
)

var testEnv = &envtest.Environment{
	CRDDirectoryPaths:     []string{filepath.Join("../../..", "config", "crd", "bases")},
	ErrorIfCRDPathMissing: true,
}

func skipIfNoEnvTest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("skipping envtest as KUBEBUILDER_ASSETS env var not found.")
	}
}

func TestApplyOperatorStatusWithEnvTest(t *testing.T) {
	skipIfNoEnvTest(t)

	cfg, err := testEnv.Start()
	require.NoError(t, err)
	defer func() {
		err = testEnv.Stop()
		require.NoError(t, err)
	}()

	clientSet, err := certmanoperatorclient.NewForConfig(cfg)
	require.NoError(t, err)

	clockStep := 5 * time.Minute
	startTs := time.Unix(0, 0)
	aheadTs1, aheadTs2, aheadTs3 := startTs.Add(clockStep), startTs.Add(clockStep*2), startTs.Add(clockStep*3)
	clock := testingclock.NewFakeClock(startTs)

	operatorClient := &OperatorClient{
		Client: clientSet.OperatorV1alpha1(),
		Clock:  clock,
	}

	commonObjectMeta := metav1.ObjectMeta{
		Name: "cluster",
	}
	commonSpec := v1alpha1.CertManagerSpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
	}

	sampleCond1 := applyoperatorv1.OperatorCondition()
	sampleCond1.WithStatus(operatorv1.ConditionTrue)
	sampleCond1.WithType("FooProgressing")
	sampleCond1.WithReason("AsExpected")
	sampleCond1.WithMessage("rolling out foobar")

	sampleCond2 := applyoperatorv1.OperatorCondition()
	sampleCond2.WithStatus(operatorv1.ConditionFalse)
	sampleCond2.WithType("FooDegraded")
	sampleCond2.WithReason("AsExpected")
	sampleCond2.WithMessage("")

	sampleGen1 := applyoperatorv1.GenerationStatus()
	sampleGen1.WithGroup("example")
	sampleGen1.WithResource("foo")
	sampleGen1.WithName("bar-1")
	sampleGen1.WithNamespace("foobar")
	sampleGen1.WithLastGeneration(99)

	sampleGen2 := applyoperatorv1.GenerationStatus()
	sampleGen2.WithGroup("example")
	sampleGen2.WithResource("foo")
	sampleGen2.WithName("bar-2")
	sampleGen2.WithNamespace("foobar")
	sampleGen2.WithLastGeneration(88)

	tests := []struct {
		name           string
		previousStatus v1alpha1.CertManagerStatus
		statusToApply  applyoperatorv1.OperatorStatusApplyConfiguration
		expectedStatus v1alpha1.CertManagerStatus
		moveClockAhead bool
	}{
		// test status.conditions
		{
			name:           "applies one condition on empty status",
			previousStatus: v1alpha1.CertManagerStatus{},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Conditions: []applyoperatorv1.OperatorConditionApplyConfiguration{
					*sampleCond1,
				},
			},
			expectedStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: []operatorv1.OperatorCondition{
						{
							Type: *sampleCond1.Type, Status: *sampleCond1.Status,
							Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
					},
				},
			},
		},
		{
			name: "applies another condition on status with an existing condition",
			previousStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: []operatorv1.OperatorCondition{
						{
							Type: *sampleCond1.Type, Status: *sampleCond1.Status,
							Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
					},
				},
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Conditions: []applyoperatorv1.OperatorConditionApplyConfiguration{
					*sampleCond2,
				},
			},
			expectedStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: []operatorv1.OperatorCondition{
						{
							Type: *sampleCond1.Type, Status: *sampleCond1.Status,
							Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
						{
							Type: *sampleCond2.Type, Status: *sampleCond2.Status,
							Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
					},
				},
			},
		},
		{
			name: "idempotent applying of two conditions",
			previousStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: []operatorv1.OperatorCondition{
						{
							Type: *sampleCond1.Type, Status: *sampleCond1.Status,
							Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
						{
							Type: *sampleCond2.Type, Status: *sampleCond2.Status,
							Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
					},
				},
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Conditions: []applyoperatorv1.OperatorConditionApplyConfiguration{
					*sampleCond2,
					*sampleCond1,
				},
			},
			expectedStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					// conditions are sorted by Type during Canonicalize
					Conditions: []operatorv1.OperatorCondition{
						{
							Type: *sampleCond2.Type, Status: *sampleCond2.Status,
							Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
							LastTransitionTime: metav1.NewTime(aheadTs1),
						},
						{
							Type: *sampleCond1.Type, Status: *sampleCond1.Status,
							Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
							LastTransitionTime: metav1.NewTime(aheadTs1),
						},
					},
				},
			},
			moveClockAhead: true,
		},
		{
			name: "applies update of two conditions",
			previousStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: []operatorv1.OperatorCondition{
						{
							Type: *sampleCond2.Type, Status: *sampleCond2.Status,
							Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
						{
							Type: *sampleCond1.Type, Status: *sampleCond1.Status,
							Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
					},
				},
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Conditions: []applyoperatorv1.OperatorConditionApplyConfiguration{
					*sampleCond1.WithStatus(operatorv1.ConditionFalse), // FooProgressing=False
					*sampleCond2.WithStatus(operatorv1.ConditionTrue),  // FooDegraded=True
				},
			},
			expectedStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: []operatorv1.OperatorCondition{
						{
							Type: *sampleCond2.Type, Status: operatorv1.ConditionTrue,
							Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
							LastTransitionTime: metav1.NewTime(aheadTs2),
						},
						{
							Type: *sampleCond1.Type, Status: operatorv1.ConditionFalse,
							Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
							LastTransitionTime: metav1.NewTime(aheadTs2),
						},
					},
				},
			},
			moveClockAhead: true,
		},

		// test status.generations
		{
			name:           "apply one generation on empty status",
			previousStatus: v1alpha1.CertManagerStatus{},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Generations: []applyoperatorv1.GenerationStatusApplyConfiguration{
					*sampleGen1,
				},
			},
			expectedStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: nil,
					Generations: []operatorv1.GenerationStatus{
						{
							Group: *sampleGen1.Group, Resource: *sampleGen1.Resource,
							Name: *sampleGen1.Name, Namespace: *sampleGen1.Namespace,
							LastGeneration: *sampleGen1.LastGeneration,
						},
					},
				},
			},
		},
		{
			name: "apply update generation on existing status",
			previousStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: nil,
					Generations: []operatorv1.GenerationStatus{
						{
							Group: *sampleGen1.Group, Resource: *sampleGen1.Resource,
							Name: *sampleGen1.Name, Namespace: *sampleGen1.Namespace,
							LastGeneration: *sampleGen1.LastGeneration,
						},
					},
				},
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Generations: []applyoperatorv1.GenerationStatusApplyConfiguration{
					*sampleGen1.WithLastGeneration(100),
				},
			},
			expectedStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: nil,
					Generations: []operatorv1.GenerationStatus{
						{
							Group: *sampleGen1.Group, Resource: *sampleGen1.Resource,
							Name: *sampleGen1.Name, Namespace: *sampleGen1.Namespace,
							LastGeneration: 100,
						},
					},
				},
			},
		},
		{
			name: "apply new generation on existing status",
			previousStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: nil,
					Generations: []operatorv1.GenerationStatus{
						{
							Group: *sampleGen1.Group, Resource: *sampleGen1.Resource,
							Name: *sampleGen1.Name, Namespace: *sampleGen1.Namespace,
							LastGeneration: *sampleGen1.LastGeneration,
						},
					},
				},
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Generations: []applyoperatorv1.GenerationStatusApplyConfiguration{
					*sampleGen2,
				},
			},
			expectedStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: nil,
					Generations: []operatorv1.GenerationStatus{
						{
							Group: *sampleGen1.Group, Resource: *sampleGen1.Resource,
							Name: *sampleGen1.Name, Namespace: *sampleGen1.Namespace,
							LastGeneration: *sampleGen1.LastGeneration,
						},
						{
							Group: *sampleGen2.Group, Resource: *sampleGen2.Resource,
							Name: *sampleGen2.Name, Namespace: *sampleGen2.Namespace,
							LastGeneration: *sampleGen2.LastGeneration,
						},
					},
				},
			},
		},

		// test status.generations and status.conditions together
		{
			name: "update one condition and one generation on existing",
			previousStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: []operatorv1.OperatorCondition{
						{
							Type: *sampleCond1.Type, Status: *sampleCond1.Status,
							Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
						{
							Type: *sampleCond2.Type, Status: *sampleCond2.Status,
							Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
					},
					Generations: []operatorv1.GenerationStatus{
						{
							Group: *sampleGen1.Group, Resource: *sampleGen1.Resource,
							Name: *sampleGen1.Name, Namespace: *sampleGen1.Namespace,
							LastGeneration: 99,
						},
						{
							Group: *sampleGen2.Group, Resource: *sampleGen2.Resource,
							Name: *sampleGen2.Name, Namespace: *sampleGen2.Namespace,
							LastGeneration: *sampleGen2.LastGeneration,
						},
					},
				},
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Generations: []applyoperatorv1.GenerationStatusApplyConfiguration{
					*sampleGen1.WithLastGeneration(100), // foobar-1: set gen 100
				},
				Conditions: []applyoperatorv1.OperatorConditionApplyConfiguration{
					*sampleCond1.WithStatus(operatorv1.ConditionFalse), // set FooProgressing=False
				},
			},
			expectedStatus: v1alpha1.CertManagerStatus{
				OperatorStatus: operatorv1.OperatorStatus{
					Conditions: []operatorv1.OperatorCondition{
						{
							Type: *sampleCond1.Type, Status: operatorv1.ConditionFalse,
							Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
							LastTransitionTime: metav1.NewTime(aheadTs3),
						},
						{
							Type: *sampleCond2.Type, Status: *sampleCond2.Status,
							Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
							LastTransitionTime: metav1.NewTime(startTs),
						},
					},
					Generations: []operatorv1.GenerationStatus{
						{
							Group: *sampleGen1.Group, Resource: *sampleGen1.Resource,
							Name: *sampleGen1.Name, Namespace: *sampleGen1.Namespace,
							LastGeneration: 100,
						},
						{
							Group: *sampleGen2.Group, Resource: *sampleGen2.Resource,
							Name: *sampleGen2.Name, Namespace: *sampleGen2.Namespace,
							LastGeneration: *sampleGen2.LastGeneration,
						},
					},
				},
			},
			moveClockAhead: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			obj, err := operatorClient.Client.CertManagers().Create(ctx, &v1alpha1.CertManager{
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,
			}, metav1.CreateOptions{})
			require.NoError(t, err)
			defer operatorClient.Client.CertManagers().Delete(ctx, commonObjectMeta.Name, metav1.DeleteOptions{})

			obj = obj.DeepCopy()
			obj.Status = tc.previousStatus
			_, err = operatorClient.Client.CertManagers().UpdateStatus(ctx, obj, metav1.UpdateOptions{})
			require.NoError(t, err)

			if tc.moveClockAhead {
				clock.Step(5 * time.Minute)
			}

			err = operatorClient.ApplyOperatorStatus(ctx, "field-manager", &tc.statusToApply)
			require.NoError(t, err)

			actual, err := operatorClient.Client.CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
			require.NoError(t, err)

			if !reflect.DeepEqual(tc.expectedStatus, actual.Status) {
				t.Fatalf("expected status mismatch, diff = %v", cmp.Diff(tc.expectedStatus, actual.Status))
			}
		})
	}
}
