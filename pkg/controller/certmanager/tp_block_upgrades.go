package certmanager

import (
	"context"

	"github.com/operator-framework/operator-lib/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	techPreviewNoUpgradeReason  = "techPreviewFeaturesUpgradeRestricted"
	techPreviewNoUpgradeMessage = "The operator installed with TechPreview features cannot be upgraded."
)

func ensureNoUpgrade(ctx context.Context, upgradeCond conditions.Condition) error {
	cond, err := upgradeCond.Get(ctx)
	if err != nil {
		return err
	}

	// upgradeable already false due to a different reason
	if cond.Status == metav1.ConditionFalse && cond.Reason != techPreviewNoUpgradeReason {
		return nil
	}

	// upgrades were already blocked, do nothing
	if cond.Message == techPreviewNoUpgradeMessage && cond.Reason == techPreviewNoUpgradeReason &&
		cond.Status == metav1.ConditionFalse {
		return nil
	}

	err = upgradeCond.Set(ctx, metav1.ConditionFalse,
		conditions.WithMessage(techPreviewNoUpgradeMessage),
		conditions.WithReason(techPreviewNoUpgradeReason))
	return err
}
