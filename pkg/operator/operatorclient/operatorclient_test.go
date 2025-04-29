package operatorclient

import (
	"context"
	"reflect"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"
	applyoperatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testingclock "k8s.io/utils/clock/testing"

	"github.com/google/go-cmp/cmp"
)

func TestApplyOperatorStatus(t *testing.T) {
	commonTypeMeta := metav1.TypeMeta{
		Kind:       "CertManager",
		APIVersion: "operator.openshift.io/v1alpha1",
	}
	commonObjectMeta := metav1.ObjectMeta{
		Name: "cluster",
	}
	commonSpec := v1alpha1.CertManagerSpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: "",
		},
	}

	clock := testingclock.NewFakeClock(time.Unix(0, 0).Add(5 * time.Minute))

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
		previousObj    v1alpha1.CertManager
		statusToApply  applyoperatorv1.OperatorStatusApplyConfiguration
		expectedObj    v1alpha1.CertManager
		moveClockAhead bool
	}{
		// test status.conditions
		{
			name: "applies one condition on empty status",
			previousObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{},
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Conditions: []applyoperatorv1.OperatorConditionApplyConfiguration{
					*sampleCond1,
				},
			},
			expectedObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type: *sampleCond1.Type, Status: *sampleCond1.Status,
								Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
						},
					},
				},
			},
		},
		{
			name: "applies another condition on status with an existing condition",
			previousObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type: *sampleCond1.Type, Status: *sampleCond1.Status,
								Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
						},
					},
				},
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Conditions: []applyoperatorv1.OperatorConditionApplyConfiguration{
					*sampleCond2,
				},
			},
			expectedObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type: *sampleCond2.Type, Status: *sampleCond2.Status,
								Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
							{
								Type: *sampleCond1.Type, Status: *sampleCond1.Status,
								Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
						},
					},
				},
			},
		},
		{
			name: "idempotent applying of two conditions",
			previousObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type: *sampleCond1.Type, Status: *sampleCond1.Status,
								Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
							{
								Type: *sampleCond2.Type, Status: *sampleCond2.Status,
								Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
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
			expectedObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						// conditions are sorted by Type during Canonicalize
						Conditions: []operatorv1.OperatorCondition{
							{
								Type: *sampleCond2.Type, Status: *sampleCond2.Status,
								Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
							{
								Type: *sampleCond1.Type, Status: *sampleCond1.Status,
								Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
						},
					},
				},
			},
			moveClockAhead: true,
		},
		{
			name: "applies update of two conditions",
			previousObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type: *sampleCond2.Type, Status: *sampleCond2.Status,
								Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
							{
								Type: *sampleCond1.Type, Status: *sampleCond1.Status,
								Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
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
			expectedObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type: *sampleCond2.Type, Status: operatorv1.ConditionTrue,
								Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
								// LastTransitionTime will be set from clock ahead time
							},
							{
								Type: *sampleCond1.Type, Status: operatorv1.ConditionFalse,
								Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
								// LastTransitionTime will be set from clock ahead time
							},
						},
					},
				},
			},
			moveClockAhead: true,
		},

		// test status.generations
		{
			name: "apply one generation on empty status",
			previousObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{},
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Generations: []applyoperatorv1.GenerationStatusApplyConfiguration{
					*sampleGen1,
				},
			},
			expectedObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
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
		},
		{
			name: "apply update generation on existing status",
			previousObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
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
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Generations: []applyoperatorv1.GenerationStatusApplyConfiguration{
					*sampleGen1.WithLastGeneration(100),
				},
			},
			expectedObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
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
		},
		{
			name: "apply new generation on existing status",
			previousObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
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
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Generations: []applyoperatorv1.GenerationStatusApplyConfiguration{
					*sampleGen2,
				},
			},
			expectedObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
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
		},

		// test status.generations and status.conditions together
		{
			name: "update one condition and one generation on existing",
			previousObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type: *sampleCond1.Type, Status: *sampleCond1.Status,
								Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
							{
								Type: *sampleCond2.Type, Status: *sampleCond2.Status,
								Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
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
			},
			statusToApply: applyoperatorv1.OperatorStatusApplyConfiguration{
				Generations: []applyoperatorv1.GenerationStatusApplyConfiguration{
					*sampleGen1.WithLastGeneration(100), // foobar-1: set gen 100
				},
				Conditions: []applyoperatorv1.OperatorConditionApplyConfiguration{
					*sampleCond1.WithStatus(operatorv1.ConditionFalse), // set FooProgressing=False
				},
			},
			expectedObj: v1alpha1.CertManager{
				TypeMeta:   commonTypeMeta,
				ObjectMeta: commonObjectMeta,
				Spec:       commonSpec,

				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type: *sampleCond2.Type, Status: *sampleCond2.Status,
								Reason: *sampleCond2.Reason, Message: *sampleCond2.Message,
								LastTransitionTime: metav1.NewTime(clock.Now()),
							},
							{
								Type: *sampleCond1.Type, Status: operatorv1.ConditionFalse,
								Reason: *sampleCond1.Reason, Message: *sampleCond1.Message,
								// LastTransitionTime will be set from clock ahead time
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
			},
			moveClockAhead: true,
		},
	}

	fakeClient := fake.NewSimpleClientset()
	operatorClient := &OperatorClient{
		Client: fakeClient.OperatorV1alpha1(),
		Clock:  clock,
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			_, err := fakeClient.OperatorV1alpha1().CertManagers().Create(ctx, &tc.previousObj, metav1.CreateOptions{})
			require.NoError(t, err)
			defer fakeClient.OperatorV1alpha1().CertManagers().Delete(ctx, tc.previousObj.Name, metav1.DeleteOptions{})

			if tc.moveClockAhead {
				clock.Step(5 * time.Minute)
			}

			err = operatorClient.ApplyOperatorStatus(ctx, "field-manager", &tc.statusToApply)
			require.NoError(t, err)

			actual, err := fakeClient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
			require.NoError(t, err)

			if tc.moveClockAhead {
				for i := range tc.expectedObj.Status.Conditions {
					tc.expectedObj.Status.Conditions[i].LastTransitionTime = metav1.NewTime(clock.Now())
				}
			}

			if !reflect.DeepEqual(tc.expectedObj, *actual) {
				t.Fatalf("expected status mismatch, diff = %v", cmp.Diff(tc.expectedObj, *actual))
			}
		})
	}
}
