package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
)

func TestGetCondition(t *testing.T) {
	tests := []struct {
		name              string
		conditionalStatus ConditionalStatus
		condition         string
		expectedCondition *metav1.Condition
	}{
		{
			name: "requested condition present",
			conditionalStatus: ConditionalStatus{
				Conditions: []metav1.Condition{
					{
						Type:   Ready,
						Status: metav1.ConditionTrue,
						Reason: ReasonReady,
					},
					{
						Type:   Degraded,
						Status: metav1.ConditionFalse,
						Reason: ReasonReady,
					},
				},
			},
			condition: Ready,
			expectedCondition: &metav1.Condition{
				Type:   Ready,
				Status: metav1.ConditionTrue,
				Reason: ReasonReady,
			},
		},
		{
			name: "requested condition not present",
			conditionalStatus: ConditionalStatus{
				Conditions: []metav1.Condition{
					{
						Type:   Ready,
						Status: metav1.ConditionTrue,
						Reason: ReasonReady,
					},
					{
						Type:   Degraded,
						Status: metav1.ConditionFalse,
						Reason: ReasonReady,
					},
				},
			},
			condition:         "TestCondition",
			expectedCondition: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := tt.conditionalStatus.GetCondition(tt.condition)
			if !reflect.DeepEqual(tt.expectedCondition, cond) {
				t.Errorf("GetCondition() condition: %v, expectedCondition: %v", cond, tt.expectedCondition)
			}
		})
	}
}

func TestSetCondition(t *testing.T) {
	tests := []struct {
		name              string
		conditionalStatus ConditionalStatus
		condition         string
		conditionStatus   metav1.ConditionStatus
		conditionReason   string
		conditionMessage  string
		conditionUpdated  bool
	}{
		{
			name: "condition does not exist",
			conditionalStatus: ConditionalStatus{
				Conditions: []metav1.Condition{
					{
						Type:   Degraded,
						Status: metav1.ConditionFalse,
						Reason: ReasonReady,
					},
				},
			},
			condition:        Ready,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  ReasonReady,
			conditionUpdated: true,
		},
		{
			name: "condition exists and status changed",
			conditionalStatus: ConditionalStatus{
				Conditions: []metav1.Condition{
					{
						Type:   Ready,
						Status: metav1.ConditionTrue,
						Reason: ReasonReady,
					},
					{
						Type:   Degraded,
						Status: metav1.ConditionFalse,
						Reason: ReasonReady,
					},
				},
			},
			condition:        Ready,
			conditionStatus:  metav1.ConditionFalse,
			conditionReason:  ReasonReady,
			conditionUpdated: true,
		},
		{
			name: "condition exists and no new change",
			conditionalStatus: ConditionalStatus{
				Conditions: []metav1.Condition{
					{
						Type:   Ready,
						Status: metav1.ConditionTrue,
						Reason: ReasonReady,
					},
					{
						Type:   Degraded,
						Status: metav1.ConditionFalse,
						Reason: ReasonReady,
					},
				},
			},
			condition:        Ready,
			conditionStatus:  metav1.ConditionTrue,
			conditionReason:  ReasonReady,
			conditionUpdated: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated := tt.conditionalStatus.SetCondition(tt.condition, tt.conditionStatus, tt.conditionReason, tt.conditionMessage)
			if updated != tt.conditionUpdated {
				t.Errorf("SetCondition() updated: %v, conditionUpdated: %v", updated, tt.conditionUpdated)
			}
		})
	}
}
