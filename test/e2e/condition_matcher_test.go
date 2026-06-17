//go:build e2e
// +build e2e

package e2e

import (
	"regexp"

	"context"
	"encoding/json"
	"fmt"
	"log"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	opv1 "github.com/openshift/api/operator/v1"
	v1alpha1client "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
)

// ConditionMatcher tracks a regex pattern cond.Type with an expected cond.Status
type ConditionMatcher struct {
	TypePattern    *regexp.Regexp       `json:"condition.Type"`
	ExpectedStatus opv1.ConditionStatus `json:"condition.Status"`

	// Any when true will expect matcher to succeed
	// when atleast one conditions match, default is false when
	// matcher will succeed only on matching all pattern conditions.
	Any bool `json:"matcher.shouldMatchAny"`
}

// MatchesType returns true if the provided Condition.Type satisfies regex pattern
func (m *ConditionMatcher) MatchesType(cond *opv1.OperatorCondition) bool {
	return m.TypePattern.MatchString(cond.Type)
}

// MatchesStatus returns true if the provided Condition.Status is same as ExpectedStatus
func (m *ConditionMatcher) MatchesStatus(cond *opv1.OperatorCondition) bool {
	return cond.Status == m.ExpectedStatus
}

func (m *ConditionMatcher) Matches(conditions []opv1.OperatorCondition) bool {
	matchCount := 0
	for _, cond := range conditions {
		if m.MatchesType(&cond) && m.MatchesStatus(&cond) {

			if m.Any {
				return true
			}

			matchCount += 1
		}

		if m.MatchesType(&cond) && !m.MatchesStatus(&cond) {
			return false
		}
	}

	return matchCount > 0
}

const (
	MatchAnyCondition  bool = true
	MatchAllConditions bool = false
)

// PrefixAndMatchTypeTuple is a tuple containing
// controller name (i.e. the condition prefix)
// and the condition matcher type be ANY or ALL.
type PrefixAndMatchTypeTuple struct {
	Prefix         string
	ShouldMatchAny bool
}

func GenerateConditionMatchers(tuples []PrefixAndMatchTypeTuple, expectedConditions map[string]opv1.ConditionStatus) []ConditionMatcher {
	matchers := make([]ConditionMatcher, len(tuples)*len(expectedConditions))

	idx := 0
	for _, t := range tuples {
		for suffix, state := range expectedConditions {
			matchers[idx] = ConditionMatcher{
				TypePattern:    regexp.MustCompile("^" + t.Prefix + ".*" + suffix + "$"),
				ExpectedStatus: state,
				Any:            t.ShouldMatchAny,
			}
			idx += 1
		}
	}
	return matchers
}

// verifyOperatorStatusCondition polls every few second to check if the status of each of the controllers
// match with the expected conditions. It returns an error if a timeout (few mins) occurs or an error was
// encountered which polls the status.
func verifyOperatorStatusCondition(client v1alpha1client.OperatorV1alpha1Interface, conditionMatchers []ConditionMatcher) error {
	var lastFetchedConditions []opv1.OperatorCondition

	err := wait.PollUntilContextTimeout(context.TODO(), fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		operator, err := client.CertManagers().Get(context.TODO(), "cluster", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		if operator.DeletionTimestamp != nil {
			return false, nil
		}

		lastFetchedConditions = operator.Status.DeepCopy().Conditions
		for _, matcher := range conditionMatchers {
			if !matcher.Matches(lastFetchedConditions) {

				// [retry] false:   NOT match as desired
				return false, nil
			}
		}

		// [no-retry] true: when ALL match as desired
		return true, nil
	})

	if err != nil {
		prettyConds, _ := json.Marshal(lastFetchedConditions)
		log.Printf("found status.conditions: %s", prettyConds)
		prettyMatchers, _ := json.Marshal(conditionMatchers)
		log.Printf("expected status.conditions to adhere with %s", prettyMatchers)

		return fmt.Errorf("could not verify all status conditions as expected : %v", err)
	}

	return nil
}

func VerifyHealthyOperatorConditions(client v1alpha1client.OperatorV1alpha1Interface) error {
	return verifyOperatorStatusCondition(client,
		GenerateConditionMatchers(
			[]PrefixAndMatchTypeTuple{
				{certManagerController, MatchAllConditions},
				{certManagerWebhook, MatchAllConditions},
				{certManagerCAInjector, MatchAllConditions},
			},
			validOperatorStatusConditions,
		),
	)
}
