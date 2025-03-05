package certmanager

import (
	"context"
	"testing"
	"time"

	apiv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAlreadyInDesiredTPConditionState(t *testing.T) {
	testCases := []struct {
		name string

		existingConditions []apiv1.OperatorCondition
		actualTPEnabled    bool

		expectedRet bool
	}{
		{
			name: "condition and TP are already false",
			existingConditions: []apiv1.OperatorCondition{
				{Type: "OtherType", Status: apiv1.ConditionTrue},
				{Type: "TechPreviewFeaturesEnabled", Status: apiv1.ConditionFalse},
			},
			actualTPEnabled: false,
			expectedRet:     true,
		},
		{
			name: "condition and TP are already true",
			existingConditions: []apiv1.OperatorCondition{
				{Type: "OtherType", Status: apiv1.ConditionUnknown},
				{Type: "TechPreviewFeaturesEnabled", Status: apiv1.ConditionTrue},
			},
			actualTPEnabled: true,
			expectedRet:     true,
		},
		{
			name: "condition is false but TP is true",
			existingConditions: []apiv1.OperatorCondition{
				{Type: "TechPreviewFeaturesEnabled", Status: apiv1.ConditionFalse},
			},
			actualTPEnabled: true,
			expectedRet:     false,
		},
		{
			name: "condition is true but TP is false",
			existingConditions: []apiv1.OperatorCondition{
				{Type: "OtherType", Status: apiv1.ConditionUnknown},
				{Type: "TechPreviewFeaturesEnabled", Status: apiv1.ConditionTrue},
				{Type: "AnotherType", Status: apiv1.ConditionFalse},
			},
			actualTPEnabled: false,
			expectedRet:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := alreadyInDesiredTPConditionState(&v1alpha1.CertManager{
				Status: v1alpha1.CertManagerStatus{
					OperatorStatus: apiv1.OperatorStatus{
						Conditions: tc.existingConditions,
					},
				},
			}, tc.actualTPEnabled)
			assert.Equal(t, tc.expectedRet, actual)
		})
	}
}

func TestSetStatusWithTPCondition(t *testing.T) {
	initialObject := &v1alpha1.CertManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: v1alpha1.CertManagerSpec{},
		Status: v1alpha1.CertManagerStatus{
			OperatorStatus: apiv1.OperatorStatus{
				Conditions: []apiv1.OperatorCondition{
					{Type: "OtherType", Status: apiv1.ConditionUnknown},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(initialObject).
		WithStatusSubresource(initialObject).
		Build()

	unixEpochTimestamp := time.Unix(0, 0)
	fakeClock := testclock.NewFakeClock(unixEpochTimestamp)
	r := &CertManagerReconciler{
		Client: fakeClient,
		scheme: scheme,
		clock:  fakeClock,
	}

	tpDisabledExpectedCondition := apiv1.OperatorCondition{
		Type:   tpConditionType,
		Status: apiv1.ConditionFalse,
		Reason: tpConditionReasonAsExpected,
	}
	tpEnabledExpectedCondition := apiv1.OperatorCondition{
		Type:    tpConditionType,
		Status:  apiv1.ConditionTrue,
		Reason:  tpConditionReasonUsed,
		Message: tpWarningMessage,
	}

	testCases := []struct {
		name                 string
		observedTPState      bool
		expectedUpdate       bool
		expectedCondition    apiv1.OperatorCondition
		shouldMoveClockAhead bool
	}{
		{
			name:            "initially no condition and tech preview is false",
			observedTPState: false, expectedUpdate: true,
			expectedCondition: tpDisabledExpectedCondition,
		},
		{
			name:            "condition is present and tech preview is enabled",
			observedTPState: true, expectedUpdate: true,
			expectedCondition: tpEnabledExpectedCondition,
		},
		{
			name:            "condition is present and tech preview was enabled previously",
			observedTPState: true, expectedUpdate: false,
			expectedCondition:    tpEnabledExpectedCondition,
			shouldMoveClockAhead: true,
		},

		// we'll not encounter this in reality, because CEL validation gates that features field once
		// set cannot be unset. However, this is a good dummy scenario to check the condition setter.
		{
			name:            "further tech preview is disabled",
			observedTPState: false, expectedUpdate: true,
			expectedCondition:    tpDisabledExpectedCondition,
			shouldMoveClockAhead: true,
		},
		{
			name:            "tech preview state unchanged and disabled",
			observedTPState: false, expectedUpdate: false,
			expectedCondition:    tpDisabledExpectedCondition,
			shouldMoveClockAhead: true,
		},
		{
			name:            "further tech preview is again enabled",
			observedTPState: true, expectedUpdate: true,
			expectedCondition: tpEnabledExpectedCondition,
		},
	}

	ctx := context.Background()
	currentObject := initialObject.DeepCopy()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.shouldMoveClockAhead {
				deltaT := time.Duration(1 * time.Hour)
				fakeClock.Step(deltaT)
			}

			actualUpdated, err := r.setStatusWithTPCondition(ctx, currentObject, tc.observedTPState)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedUpdate, actualUpdated)

			tc.expectedCondition.LastTransitionTime = metav1.NewTime(fakeClock.Now())
			if !tc.expectedUpdate &&
				currentObject.Status.OperatorStatus.Conditions[1].Type == tpConditionType {

				tc.expectedCondition.LastTransitionTime = currentObject.Status.OperatorStatus.Conditions[1].LastTransitionTime
			}

			err = r.Get(ctx, client.ObjectKeyFromObject(currentObject), currentObject)
			require.NoError(t, err)
			assert.Contains(t, currentObject.Status.OperatorStatus.Conditions, tc.expectedCondition)
		})
	}
}
