package certmanager

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	operatorconfigclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

const (
	autoApproverControllerName   = "CertManagerAutoApprover"
	autoApproverRBACName         = "cert-manager-controller-approve:cert-manager-io"
	autoApproverRoleAsset        = "cert-manager-deployment/cert-manager/cert-manager-controller-approve-cert-manager-io-cr.yaml"
	autoApproverBindingAsset     = "cert-manager-deployment/cert-manager/cert-manager-controller-approve-cert-manager-io-crb.yaml"
	certificateRequestApprover   = "certificaterequests-approver"
	autoApproverDisableRequested = "AutoApproverDisableRequested"
)

// CertManagerAutoApproverController owns the built-in approver's RBAC and records
// disable intent separately from the completed handoff. The existing Deployment
// controller remains the only writer of the Deployment.
// This loop runs independently of the operand feature gate: turning that gate off
// must not release an established latch while ApproverPolicyManager still exists.
type CertManagerAutoApproverController struct {
	configClient operatorconfigclient.OperatorV1alpha1Interface
	kubeClient   kubernetes.Interface
}

func NewCertManagerAutoApproverController(
	operatorClient v1helpers.OperatorClient,
	configClient operatorconfigclient.OperatorV1alpha1Interface,
	operatorInformers certmanoperatorinformers.SharedInformerFactory,
	kubeClient kubernetes.Interface,
	kubeInformers v1helpers.KubeInformersForNamespaces,
	recorder events.Recorder,
) factory.Controller {
	policies := operatorInformers.Operator().V1alpha1().ApproverPolicyManagers()
	c := &CertManagerAutoApproverController{
		configClient: configClient,
		kubeClient:   kubeClient,
	}
	return factory.New().WithInformers(
		operatorClient.Informer(),
		// Status-only updates must enqueue the handoff, not just spec changes.
		policies.Informer(),
		kubeInformers.InformersFor(operatorclient.TargetNamespace).Apps().V1().Deployments().Informer(),
		kubeInformers.InformersFor(operatorclient.TargetNamespace).Core().V1().Pods().Informer(),
		kubeInformers.InformersFor("").Rbac().V1().ClusterRoles().Informer(),
		kubeInformers.InformersFor("").Rbac().V1().ClusterRoleBindings().Informer(),
	).ResyncEvery(time.Minute).WithSync(c.sync).WithSyncDegradedOnError(operatorClient).
		ToController(autoApproverControllerName, recorder.WithComponentSuffix("auto-approver"))
}

// autoApproverState decides whether built-in approval should be disabled,
// retaining persisted intent through readiness failures and finalization.
// The policy's controller reports Ready after checking its Deployment.
// Handoff completion is checked separately. A nil policy means confirmed NotFound.
func autoApproverState(cm *v1alpha1.CertManager, policy *v1alpha1.ApproverPolicyManager) bool {
	if policy == nil {
		return false
	}
	ready := meta.FindStatusCondition(policy.Status.Conditions, v1alpha1.Ready)
	return disableIntentPersisted(cm) || (ready != nil && ready.Status == metav1.ConditionTrue && ready.ObservedGeneration == policy.Generation)
}

func disableIntentPersisted(cm *v1alpha1.CertManager) bool {
	if condition := v1helpers.FindOperatorCondition(cm.Status.Conditions, autoApproverDisableRequested); condition != nil {
		return condition.Status == operatorv1.ConditionTrue
	}
	// Preserve a completed handoff recorded before the intent condition existed.
	return cm.Status.AutoApproverDisabled
}

// approverControllerSelection injects the disabling selection only when needed.
// Compatible explicit controller selections are preserved unchanged.
func approverControllerSelection(args []string, disabled bool) (string, error) {
	argMap := map[string]string{}
	common.ParseArgMap(argMap, args)
	selection, found := argMap["--controllers"]
	if !found {
		selection = "*"
	}
	if !disabled {
		return selection, nil
	}
	enabled, err := autoApproverEnabled(selection)
	if err != nil {
		return "", err
	}
	if !enabled {
		return selection, nil
	}
	return selection + ",-" + certificateRequestApprover, nil
}

// parseControllersSelection decodes a --controllers value the same way pflag's
// StringSlice / cert-manager do: as a single CSV record. Quoted fields are
// unescaped (e.g. `"*"` → `*`). Malformed input is returned as an error.
func parseControllersSelection(selection string) ([]string, error) {
	if selection == "" {
		return nil, nil
	}
	controllers, err := csv.NewReader(strings.NewReader(selection)).Read()
	if err != nil {
		return nil, fmt.Errorf("invalid --controllers value %q: %w", selection, err)
	}
	return controllers, nil
}

// Match cert-manager's selection rules: exclusions win regardless of ordering,
// and a list containing only exclusions implicitly enables the default set.
// Selection values are CSV-decoded to match the operand's flag parser.
func autoApproverEnabled(selection string) (bool, error) {
	controllers, err := parseControllersSelection(selection)
	if err != nil {
		return false, err
	}
	if slices.Contains(controllers, "-"+certificateRequestApprover) {
		return false, nil
	}
	if slices.Contains(controllers, "*") || slices.Contains(controllers, certificateRequestApprover) {
		return true, nil
	}
	for _, controller := range controllers {
		if !strings.HasPrefix(controller, "-") {
			return false, nil
		}
	}
	return true, nil
}

func desiredAutoApproverSelection(cm *v1alpha1.CertManager, disabled bool) (string, error) {
	var args []string
	if cm.Spec.ControllerConfig != nil {
		args = cm.Spec.ControllerConfig.OverrideArgs
	}
	overrides, err := operatorclient.GetUnsupportedConfigOverrides(&cm.Spec.OperatorSpec)
	if err != nil {
		return "", err
	}
	if overrides != nil {
		args = common.MergeContainerArgs(args, overrides.Controller.Args)
	}
	argMap := map[string]string{}
	common.ParseArgMap(argMap, args)
	if selection, overridden := argMap["--controllers"]; disabled && overridden {
		enabled, err := autoApproverEnabled(selection)
		if err != nil {
			return "", err
		}
		if enabled {
			return "", fmt.Errorf("AutoApproverOverrideConflict: --controllers=%s enables %s while ApproverPolicyManager disable intent is active; remove the controller selection override or exclude %s", selection, certificateRequestApprover, certificateRequestApprover)
		}
	}
	return approverControllerSelection(args, disabled)
}

func withAutoApprover(informers certmanoperatorinformers.SharedInformerFactory, kubeClient kubernetes.Interface) deploymentcontroller.DeploymentHookFunc {
	return func(_ *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		cm, err := informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
		if err != nil {
			return err
		}
		// Only durable intent may change the Deployment; live Ready can flicker
		// before the coordinator records completion.
		if !disableIntentPersisted(cm) {
			return nil
		}
		selection, err := desiredAutoApproverSelection(cm, true)
		if err != nil {
			return err
		}
		// Intent is published before RBAC deletion. This independent writer must
		// not advance the handoff until both approval resources are actually gone.
		if err := checkApprovalRBACAbsent(context.TODO(), kubeClient); err != nil {
			return err
		}
		container := &deployment.Spec.Template.Spec.Containers[0]
		container.Args = common.MergeContainerArgs(container.Args, []string{"--controllers=" + selection})
		return nil
	}
}

func (c *CertManagerAutoApproverController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// Read the complete status directly so other library-go writers' conditions,
	// generations and versions survive the typed UpdateStatus below.
	cm, err := c.configClient.CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if cm.Spec.ManagementState != operatorv1.Managed || !cm.DeletionTimestamp.IsZero() {
		return nil
	}
	// A cache miss must not release persisted intent. Only an API-server
	// NotFound confirms that ApproverPolicyManager and its finalization are gone.
	policy, err := c.configClient.ApproverPolicyManagers().Get(ctx, "cluster", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		policy = nil
	} else if err != nil {
		return err
	}
	shouldDisable := autoApproverState(cm, policy)
	cm, err = c.persistDisableIntent(ctx, cm, shouldDisable)
	if err != nil {
		return err
	}
	if err := c.reconcileRBAC(ctx, syncCtx.Recorder(), shouldDisable); err != nil {
		return err
	}

	expected, err := desiredAutoApproverSelection(cm, shouldDisable)
	if err != nil {
		return err
	}
	deployment, err := c.kubeClient.AppsV1().Deployments(operatorclient.TargetNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// Wait for the Deployment controller to create or recreate the operand.
		syncCtx.Queue().AddAfter(syncCtx.QueueKey(), time.Second)
		return nil
	}
	if err != nil {
		return err
	}
	actual, err := approverControllerSelection(deployment.Spec.Template.Spec.Containers[0].Args, false)
	if err != nil {
		return err
	}
	if actual != expected {
		// Deployment apply errors are retried by its existing controller. Wait for
		// its successful write before publishing the completed transition.
		syncCtx.Queue().AddAfter(syncCtx.QueueKey(), time.Second)
		return nil
	}
	if policy != nil && !shouldDisable {
		syncCtx.Queue().AddAfter(syncCtx.QueueKey(), 30*time.Second)
	}
	if cm.Status.AutoApproverDisabled == shouldDisable {
		return nil
	}
	if shouldDisable {
		complete, err := c.disableRolloutComplete(ctx, deployment)
		if err != nil {
			return err
		}
		if !complete {
			syncCtx.Queue().AddAfter(syncCtx.QueueKey(), time.Second)
			return nil
		}
		// Recheck after observing rollout and Pods; approval resources may have
		// reappeared since the initial deletion earlier in this reconciliation.
		if err := checkApprovalRBACAbsent(ctx, c.kubeClient); err != nil {
			return err
		}
	}
	cm.Status.AutoApproverDisabled = shouldDisable
	if _, err := c.configClient.CertManagers().UpdateStatus(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return err
	}
	// Emit only after the status transition succeeds, and attach the Event to
	// CertManager rather than the operator Pod used by the shared recorder.
	// Events for cluster-scoped objects are stored in the default namespace.
	recorder := events.NewRecorder(c.kubeClient.CoreV1().Events(metav1.NamespaceDefault), autoApproverControllerName, &corev1.ObjectReference{
		APIVersion: v1alpha1.SchemeGroupVersion.String(), Kind: "CertManager", Name: cm.Name, UID: cm.UID,
	}, clock.RealClock{}).WithContext(ctx)
	if shouldDisable {
		recorder.Event("AutoApproverDisabled", "built-in CertificateRequest auto-approver has been disabled; approver-policy is now responsible for approvals")
	} else {
		recorder.Event("AutoApproverEnabled", "built-in CertificateRequest auto-approver has been re-enabled")
	}
	return nil
}

func (c *CertManagerAutoApproverController) disableRolloutComplete(ctx context.Context, deployment *appsv1.Deployment) (bool, error) {
	replicas := ptr.Deref(deployment.Spec.Replicas, 1)
	if !deployment.DeletionTimestamp.IsZero() || deployment.Status.ObservedGeneration < deployment.Generation ||
		deployment.Status.UpdatedReplicas != replicas || deployment.Status.Replicas != replicas || deployment.Status.AvailableReplicas < replicas {
		return false, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return false, fmt.Errorf("invalid cert-manager Deployment Pod selector: %w", err)
	}
	pods, err := c.kubeClient.CoreV1().Pods(deployment.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return false, err
	}
	for _, pod := range pods.Items {
		// Do not skip terminating Pods: they may still be running the approver.
		for _, container := range pod.Spec.Containers {
			if container.Name != deployment.Spec.Template.Spec.Containers[0].Name {
				continue
			}
			selection, err := approverControllerSelection(container.Args, false)
			if err != nil {
				return false, err
			}
			enabled, err := autoApproverEnabled(selection)
			if err != nil {
				return false, fmt.Errorf("cannot determine auto-approver state for Pod %s/%s: %w", pod.Namespace, pod.Name, err)
			}
			if enabled {
				return false, nil
			}
		}
	}
	return true, nil
}

func (c *CertManagerAutoApproverController) persistDisableIntent(ctx context.Context, cm *v1alpha1.CertManager, disabled bool) (*v1alpha1.CertManager, error) {
	condition := v1helpers.FindOperatorCondition(cm.Status.Conditions, autoApproverDisableRequested)
	if condition == nil && !disabled && !cm.Status.AutoApproverDisabled {
		return cm, nil
	}
	desired := operatorv1.OperatorCondition{
		Type: autoApproverDisableRequested, Status: operatorv1.ConditionTrue,
		Reason: "ApproverPolicyHandoff", Message: "Disable built-in approval until ApproverPolicyManager is deleted",
	}
	if !disabled {
		// Keep an explicit False while restoration is in progress: the completion
		// field can still be True until RBAC and the Deployment are restored.
		desired.Status = operatorv1.ConditionFalse
		desired.Reason = "ApproverPolicyNotFound"
		desired.Message = "ApproverPolicyManager deletion confirmed; restore built-in approval"
	}
	if condition != nil && condition.Status == desired.Status {
		return cm, nil
	}
	v1helpers.SetOperatorCondition(&cm.Status.Conditions, desired)
	return c.configClient.CertManagers().UpdateStatus(ctx, cm, metav1.UpdateOptions{})
}

func (c *CertManagerAutoApproverController) reconcileRBAC(ctx context.Context, recorder events.Recorder, disabled bool) error {
	rbac := c.kubeClient.RbacV1()
	if !disabled {
		_, _, roleErr := resourceapply.ApplyClusterRole(ctx, rbac, recorder, resourceread.ReadClusterRoleV1OrDie(assets.MustAsset(autoApproverRoleAsset)))
		_, _, bindingErr := resourceapply.ApplyClusterRoleBinding(ctx, rbac, recorder, resourceread.ReadClusterRoleBindingV1OrDie(assets.MustAsset(autoApproverBindingAsset)))
		return errors.Join(roleErr, bindingErr)
	}
	// Delete the binding first, then the role. Try both even if one fails, and
	// confirm absence since finalizers can delay deletion.
	bindingErr := rbac.ClusterRoleBindings().Delete(ctx, autoApproverRBACName, metav1.DeleteOptions{})
	roleErr := rbac.ClusterRoles().Delete(ctx, autoApproverRBACName, metav1.DeleteOptions{})
	if err := errors.Join(ignoreNotFound(bindingErr), ignoreNotFound(roleErr)); err != nil {
		return err
	}
	return checkApprovalRBACAbsent(ctx, c.kubeClient)
}

func checkApprovalRBACAbsent(ctx context.Context, kubeClient kubernetes.Interface) error {
	_, bindingErr := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, autoApproverRBACName, metav1.GetOptions{})
	if bindingErr == nil {
		bindingErr = fmt.Errorf("waiting for auto-approver ClusterRoleBinding deletion")
	}
	_, roleErr := kubeClient.RbacV1().ClusterRoles().Get(ctx, autoApproverRBACName, metav1.GetOptions{})
	if roleErr == nil {
		roleErr = fmt.Errorf("waiting for auto-approver ClusterRole deletion")
	}
	return errors.Join(ignoreNotFound(bindingErr), ignoreNotFound(roleErr))
}

func ignoreNotFound(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
