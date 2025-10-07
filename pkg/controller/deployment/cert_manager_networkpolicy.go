package deployment

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/staticresourcecontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
)

const (
	certManagerNetworkPolicyStaticResourcesControllerName = operatorName + "-networkpolicy-static-resources-"
	certManagerNetworkPolicyUserDefinedControllerName     = operatorName + "-networkpolicy-user-defined"
	certManagerNamespace                                  = "cert-manager"
	networkPolicyOwnerLabel                               = "cert-manager.operator.openshift.io/owned-by"
)

var (
	// Static network policy asset files for default policies
	certManagerNetworkPolicyAssetFiles = []string{
		"networkpolicies/cert-manager-deny-all-networkpolicy.yaml",
		"networkpolicies/cert-manager-allow-egress-to-api-server-networkpolicy.yaml",
		"networkpolicies/cert-manager-allow-ingress-to-metrics-networkpolicy.yaml",
		"networkpolicies/cert-manager-allow-ingress-to-webhook-networkpolicy.yaml",
		"networkpolicies/cert-manager-allow-egress-to-dns-networkpolicy.yaml",
	}
)

// ============================================================================
// STATIC RESOURCE CONTROLLER - for default network policies from YAML files
// ============================================================================

func NewCertManagerNetworkPolicyStaticResourcesController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer *resourceapply.ClientHolder,
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces,
	certManagerOperatorInformers certmanoperatorinformers.SharedInformerFactory,
	eventsRecorder events.Recorder) factory.Controller {

	// Create conditional function to check if network policies should be applied
	shouldApplyNetworkPolicies := func() bool {
		certManager, err := certManagerOperatorInformers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
		if err != nil {
			return false
		}
		return certManager.Spec.DefaultNetworkPolicy == "true"
	}

	return staticresourcecontroller.NewStaticResourceController(
		certManagerNetworkPolicyStaticResourcesControllerName,
		assets.Asset,
		[]string{}, // empty files, we'll add them conditionally
		kubeClientContainer,
		operatorClient,
		eventsRecorder,
	).WithConditionalResources(
		assets.Asset,
		certManagerNetworkPolicyAssetFiles,
		shouldApplyNetworkPolicies,
		nil, // Since immutable, we never delete
	).AddKubeInformers(kubeInformersForNamespaces).AddInformer(
		certManagerOperatorInformers.Operator().V1alpha1().CertManagers().Informer(),
	)
}

// ============================================================================
// USER-DEFINED CONTROLLER - for user-configured network policies from API
// ============================================================================

// CertManagerNetworkPolicyUserDefinedController manages user-defined NetworkPolicy resources
type CertManagerNetworkPolicyUserDefinedController struct {
	operatorClient               v1helpers.OperatorClient
	certManagerOperatorInformers certmanoperatorinformers.SharedInformerFactory
	kubeClient                   kubernetes.Interface
	eventRecorder                events.Recorder
}

func NewCertManagerNetworkPolicyUserDefinedController(
	operatorClient v1helpers.OperatorClient,
	certManagerOperatorInformers certmanoperatorinformers.SharedInformerFactory,
	kubeClient kubernetes.Interface,
	eventRecorder events.Recorder,
) factory.Controller {
	c := &CertManagerNetworkPolicyUserDefinedController{
		operatorClient:               operatorClient,
		certManagerOperatorInformers: certManagerOperatorInformers,
		kubeClient:                   kubeClient,
		eventRecorder:                eventRecorder.WithComponentSuffix("cert-manager-networkpolicy-user-defined"),
	}

	return factory.New().
		WithInformers(
			operatorClient.Informer(),
			certManagerOperatorInformers.Operator().V1alpha1().CertManagers().Informer(),
		).
		WithSync(c.sync).
		ToController(certManagerNetworkPolicyUserDefinedControllerName, c.eventRecorder)
}

func (c *CertManagerNetworkPolicyUserDefinedController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// Get the current CertManager configuration
	certManager, err := c.certManagerOperatorInformers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		if errors.IsNotFound(err) {
			// No CertManager found, nothing to do
			return nil
		}
		return fmt.Errorf("failed to get CertManager: %w", err)
	}

	// Check if network policies are enabled
	if certManager.Spec.DefaultNetworkPolicy != "true" {
		// Network policies not enabled, nothing to do
		// Note: Since fields are immutable, no cleanup needed
		return nil
	}

	// Validate user-defined network policy configuration
	if err := c.validateNetworkPolicyConfig(certManager); err != nil {
		c.eventRecorder.Warningf("NetworkPolicyValidationFailed", "Network policy configuration validation failed: %v", err)
		return fmt.Errorf("network policy configuration validation failed: %w", err)
	}

	// Apply user-defined network policies
	if err := c.reconcileUserNetworkPolicies(ctx, certManager); err != nil {
		c.eventRecorder.Warningf("UserNetworkPolicyReconcileFailed", "Failed to reconcile user network policies: %v", err)
		return fmt.Errorf("failed to reconcile user network policies: %w", err)
	}

	c.eventRecorder.Event("UserNetworkPolicyReconcileSuccess", "Successfully reconciled user-defined network policies")
	return nil
}

func (c *CertManagerNetworkPolicyUserDefinedController) validateNetworkPolicyConfig(certManager *v1alpha1.CertManager) error {
	// Validate each user-defined network policy
	for i, policy := range certManager.Spec.NetworkPolicies {
		if policy.Name == "" {
			return fmt.Errorf("network policy at index %d: name cannot be empty", i)
		}
		// Note: Empty egress rules are allowed and create a deny-all egress policy
		if err := c.validateComponentName(policy.ComponentName); err != nil {
			return fmt.Errorf("network policy at index %d: invalid component name: %w", i, err)
		}
	}
	return nil
}

func (c *CertManagerNetworkPolicyUserDefinedController) validateComponentName(componentName v1alpha1.ComponentName) error {
	switch componentName {
	case v1alpha1.CAInjector, v1alpha1.CoreController, v1alpha1.Webhook:
		return nil
	default:
		return fmt.Errorf("unsupported component name: %s", componentName)
	}
}

func (c *CertManagerNetworkPolicyUserDefinedController) reconcileUserNetworkPolicies(ctx context.Context, certManager *v1alpha1.CertManager) error {
	// Apply each user-defined network policy
	for _, userPolicy := range certManager.Spec.NetworkPolicies {
		policy := c.createUserNetworkPolicy(userPolicy)
		if err := c.createOrUpdateNetworkPolicy(ctx, policy); err != nil {
			return fmt.Errorf("failed to create/update user network policy %s: %w", policy.Name, err)
		}
	}

	return nil
}

func (c *CertManagerNetworkPolicyUserDefinedController) createUserNetworkPolicy(userPolicy v1alpha1.NetworkPolicy) *networkingv1.NetworkPolicy {
	podSelector := c.getPodSelectorForComponent(userPolicy.ComponentName)

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("cert-manager-user-%s", userPolicy.Name),
			Namespace: certManagerNamespace,
			Labels: map[string]string{
				networkPolicyOwnerLabel: "cert-manager",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: podSelector,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: userPolicy.Egress,
		},
	}
}

func (c *CertManagerNetworkPolicyUserDefinedController) getPodSelectorForComponent(component v1alpha1.ComponentName) metav1.LabelSelector {
	switch component {
	case v1alpha1.CAInjector:
		return metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "cainjector",
			},
		}
	case v1alpha1.CoreController:
		return metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "cert-manager",
			},
		}
	case v1alpha1.Webhook:
		return metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": "webhook",
			},
		}
	default:
		return metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/name": "cert-manager",
			},
		}
	}
}

func (c *CertManagerNetworkPolicyUserDefinedController) createOrUpdateNetworkPolicy(ctx context.Context, policy *networkingv1.NetworkPolicy) error {
	existing, err := c.kubeClient.NetworkingV1().NetworkPolicies(policy.Namespace).Get(ctx, policy.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new policy
			_, err := c.kubeClient.NetworkingV1().NetworkPolicies(policy.Namespace).Create(ctx, policy, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create network policy: %w", err)
			}
			c.eventRecorder.Eventf("NetworkPolicyCreated", "Created user-defined network policy %s", policy.Name)
			return nil
		}
		return fmt.Errorf("failed to get existing network policy: %w", err)
	}

	// Update existing policy
	existing.Spec = policy.Spec
	existing.Labels = policy.Labels
	_, err = c.kubeClient.NetworkingV1().NetworkPolicies(policy.Namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update network policy: %w", err)
	}

	c.eventRecorder.Eventf("NetworkPolicyUpdated", "Updated user-defined network policy %s", policy.Name)

	return nil
}
