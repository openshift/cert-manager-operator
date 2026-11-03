package certmanager

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	applyoperatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	operatorclients "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	operatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
	"github.com/openshift/cert-manager-operator/pkg/operator/utils"
)

// Real informers and API status subresources are necessary to prove that a
// status-only readiness change drives the handoff and that GA status writers
// preserve the latch. Real RBAC verifies capability removal, not just a flag.
func TestAutoApproverIntegration(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("set KUBEBUILDER_ASSETS to run auto-approver integration")
	}
	// library-go otherwise derives its finalizer prefix from the test binary path.
	t.Setenv("OPERATOR_NAME", "cert-manager")
	env := &envtest.Environment{
		CRDDirectoryPaths: []string{
			"../../../config/crd/bases/operator.openshift.io_certmanagers.yaml",
			"../../../config/crd/bases/operator.openshift.io_approverpolicymanagers.yaml",
		},
		ErrorIfCRDPathMissing: true,
	}
	config, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	kube, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	operator, err := operatorclients.NewForConfig(config)
	require.NoError(t, err)
	for _, name := range []string{operatorclient.TargetNamespace, common.OperatorNamespace} {
		_, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{})
		require.NoError(t, err)
	}
	cmClient := operator.OperatorV1alpha1().CertManagers()
	cm, err := cmClient.Create(ctx, &v1alpha1.CertManager{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       v1alpha1.CertManagerSpec{OperatorSpec: operatorv1.OperatorSpec{ManagementState: operatorv1.Managed}},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	cm.Status.ObservedGeneration = 7
	_, err = cmClient.UpdateStatus(ctx, cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	operatorInformers := operatorinformers.NewSharedInformerFactory(operator, 0)
	kubeInformers := v1helpers.NewKubeInformersForNamespaces(kube, "", "kube-system", operatorclient.TargetNamespace)
	opClient := &operatorclient.OperatorClient{Informers: operatorInformers, Client: operator.OperatorV1alpha1(), Clock: clock.RealClock{}}
	recorder := events.NewInMemoryRecorder("auto-approver-integration", clock.RealClock{})
	controllers := []factory.Controller{
		NewCertManagerControllerStaticResourcesController(opClient, resourceapply.NewKubeClientHolder(kube), kubeInformers, recorder),
		NewCertManagerControllerDeploymentController(opClient, operatorInformers, utils.OptionalInformer[configinformers.SharedInformerFactory]{}, kube, kubeInformers.InformersFor(operatorclient.TargetNamespace), recorder, "test", status.NewVersionGetter(), "", ""),
		NewCertManagerAutoApproverController(opClient, operator.OperatorV1alpha1(), operatorInformers, kube, kubeInformers, recorder),
	}
	operatorInformers.Start(ctx.Done())
	kubeInformers.Start(ctx.Done())
	operatorInformers.WaitForCacheSync(ctx.Done())
	kubeInformers.WaitForCacheSync(ctx.Done())
	go controllers[0].Run(ctx, 1)
	handoffCtx, stopHandoff := context.WithCancel(ctx)
	t.Cleanup(stopHandoff)
	handoffStopped := make(chan struct{})
	go func() {
		controllers[2].Run(handoffCtx, 1)
		close(handoffStopped)
	}()
	// Create the initial Deployment, then hold its controller until we have
	// exercised a readiness failure while the handoff is still incomplete.
	deploymentSync := factory.NewSyncContext("deployment", recorder)
	t.Cleanup(deploymentSync.Queue().ShutDown)
	require.NoError(t, controllers[1].Sync(ctx, deploymentSync))

	checkState := func(t *testing.T, disabled bool) {
		t.Helper()
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			cm, err := cmClient.Get(ctx, "cluster", metav1.GetOptions{})
			require.NoError(c, err)
			assert.Equal(c, disabled, cm.Status.AutoApproverDisabled)
			assert.Equal(c, disabled, v1helpers.IsOperatorConditionTrue(cm.Status.Conditions, autoApproverDisableRequested))
			assert.EqualValues(c, 7, cm.Status.ObservedGeneration)
			deployment, err := kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
			require.NoError(c, err)
			expected := "*"
			if disabled {
				expected += ",-certificaterequests-approver"
			}
			assert.Equal(c, expected, approverControllerSelection(deployment.Spec.Template.Spec.Containers[0].Args, false))
			_, roleErr := kube.RbacV1().ClusterRoles().Get(ctx, autoApproverRBACName, metav1.GetOptions{})
			_, bindingErr := kube.RbacV1().ClusterRoleBindings().Get(ctx, autoApproverRBACName, metav1.GetOptions{})
			if disabled {
				assert.True(c, apierrors.IsNotFound(roleErr))
				assert.True(c, apierrors.IsNotFound(bindingErr))
			} else {
				assert.NoError(c, roleErr)
				assert.NoError(c, bindingErr)
			}
			access, err := kube.AuthorizationV1().SubjectAccessReviews().Create(ctx, &authorizationv1.SubjectAccessReview{Spec: authorizationv1.SubjectAccessReviewSpec{
				User: "system:serviceaccount:cert-manager:cert-manager",
				// cert-manager's webhook checks this literal wildcard signer name
				// as well as the specific signer; Kubernetes does not glob resourceNames.
				ResourceAttributes: &authorizationv1.ResourceAttributes{Group: "cert-manager.io", Resource: "signers", Verb: "approve", Name: "issuers.cert-manager.io/*"},
			}}, metav1.CreateOptions{})
			require.NoError(c, err)
			assert.Equal(c, !disabled, access.Status.Allowed)
		}, 20*time.Second, 100*time.Millisecond)
	}
	checkState(t, false)
	setOverride := func(raw string) {
		t.Helper()
		require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current, err := cmClient.Get(ctx, "cluster", metav1.GetOptions{})
			if err != nil {
				return err
			}
			current.Spec.UnsupportedConfigOverrides.Raw = []byte(raw)
			if raw == "" {
				current.Spec.UnsupportedConfigOverrides.Raw = nil
			}
			_, err = cmClient.Update(ctx, current, metav1.UpdateOptions{})
			return err
		}))
	}
	checkConflict := func(expected bool, conditionTypes ...string) {
		t.Helper()
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			current, err := cmClient.Get(ctx, "cluster", metav1.GetOptions{})
			require.NoError(c, err)
			for _, conditionType := range conditionTypes {
				condition := v1helpers.FindOperatorCondition(current.Status.Conditions, conditionType)
				require.NotNil(c, condition)
				assert.Equal(c, expected, condition.Status == operatorv1.ConditionTrue)
				assert.Equal(c, expected, strings.Contains(condition.Message, "AutoApproverOverrideConflict"))
			}
		}, 20*time.Second, 100*time.Millisecond)
	}

	policyClient := operator.OperatorV1alpha1().ApproverPolicyManagers()
	policy, err := policyClient.Create(ctx, &v1alpha1.ApproverPolicyManager{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Finalizers: []string{"test.example/hold-cleanup"}}}, metav1.CreateOptions{})
	require.NoError(t, err)
	checkState(t, false)
	setOverride(`{"controller":{"args":["--controllers=*"]}}`)

	// Only a status update, with unchanged metadata.generation, initiates handoff.
	policy.Status.Conditions = []metav1.Condition{{Type: v1alpha1.Ready, Status: metav1.ConditionTrue, Reason: "Ready", Message: "ready", ObservedGeneration: policy.Generation, LastTransitionTime: metav1.Now()}}
	policy, err = policyClient.UpdateStatus(ctx, policy, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		cm, err := cmClient.Get(ctx, "cluster", metav1.GetOptions{})
		require.NoError(c, err)
		assert.True(c, v1helpers.IsOperatorConditionTrue(cm.Status.Conditions, autoApproverDisableRequested))
		assert.False(c, cm.Status.AutoApproverDisabled)
		_, roleErr := kube.RbacV1().ClusterRoles().Get(ctx, autoApproverRBACName, metav1.GetOptions{})
		_, bindingErr := kube.RbacV1().ClusterRoleBindings().Get(ctx, autoApproverRBACName, metav1.GetOptions{})
		assert.True(c, apierrors.IsNotFound(roleErr))
		assert.True(c, apierrors.IsNotFound(bindingErr))
	}, 20*time.Second, 100*time.Millisecond)
	checkConflict(true, autoApproverControllerName+"Degraded")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		err := controllers[1].Sync(ctx, deploymentSync)
		require.ErrorContains(c, err, "AutoApproverOverrideConflict")
	}, 5*time.Second, 100*time.Millisecond)
	blockedDeployment, err := kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "*", approverControllerSelection(blockedDeployment.Spec.Template.Spec.Containers[0].Args, false))

	// Restart the coordinator after Ready drops, before the Deployment changes
	// or completion is recorded. Only the API-persisted intent survives.
	stopHandoff()
	select {
	case <-handoffStopped:
	case <-time.After(5 * time.Second):
		t.Fatal("handoff controller did not stop")
	}
	policy.Status.Conditions[0].Status = metav1.ConditionFalse
	policy, err = policyClient.UpdateStatus(ctx, policy, metav1.UpdateOptions{})
	require.NoError(t, err)
	setOverride("")
	controllers[2] = NewCertManagerAutoApproverController(opClient, operator.OperatorV1alpha1(), operatorInformers, kube, kubeInformers, recorder)
	// Reconcile once before allowing Deployment changes so RBAC restoration
	// cannot be hidden by a fast completion write from the resumed controllers.
	handoffSync := factory.NewSyncContext("restarted-handoff", recorder)
	t.Cleanup(handoffSync.Queue().ShutDown)
	require.NoError(t, controllers[2].Sync(ctx, handoffSync))
	_, err = kube.RbacV1().ClusterRoles().Get(ctx, autoApproverRBACName, metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err))
	_, err = kube.RbacV1().ClusterRoleBindings().Get(ctx, autoApproverRBACName, metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err))
	go controllers[2].Run(ctx, 1)
	go controllers[1].Run(ctx, 1)

	// envtest has no Deployment controller or kubelet. Model an old Pod held in
	// termination, then explicitly report the new Deployment's rollout status.
	oldPod, err := kube.CoreV1().Pods(operatorclient.TargetNamespace).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "old-auto-approver", Labels: blockedDeployment.Spec.Selector.MatchLabels, Finalizers: []string{"test.example/hold-termination"}},
		Spec: corev1.PodSpec{ServiceAccountName: blockedDeployment.Spec.Template.Spec.ServiceAccountName,
			Containers: []corev1.Container{{Name: blockedDeployment.Spec.Template.Spec.Containers[0].Name, Image: "example.com/cert-manager:test"}}},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, kube.CoreV1().Pods(oldPod.Namespace).Delete(ctx, oldPod.Name, metav1.DeleteOptions{}))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		deployment, err := kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
		require.NoError(c, err)
		assert.Equal(c, "*,-certificaterequests-approver", approverControllerSelection(deployment.Spec.Template.Spec.Containers[0].Args, false))
	}, 20*time.Second, 100*time.Millisecond)
	current, err := cmClient.Get(ctx, "cluster", metav1.GetOptions{})
	require.NoError(t, err)
	assert.False(t, current.Status.AutoApproverDisabled)
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		deployment, err := kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
		if err != nil {
			return err
		}
		replicas := ptr.Deref(deployment.Spec.Replicas, 1)
		deployment.Status = appsv1.DeploymentStatus{ObservedGeneration: deployment.Generation, Replicas: replicas, UpdatedReplicas: replicas, ReadyReplicas: replicas, AvailableReplicas: replicas}
		_, err = kube.AppsV1().Deployments(deployment.Namespace).UpdateStatus(ctx, deployment, metav1.UpdateOptions{})
		return err
	}))
	require.NoError(t, controllers[2].Sync(ctx, handoffSync))
	current, err = cmClient.Get(ctx, "cluster", metav1.GetOptions{})
	require.NoError(t, err)
	assert.False(t, current.Status.AutoApproverDisabled, "terminating approver-enabled Pod must block completion")
	oldPod, err = kube.CoreV1().Pods(oldPod.Namespace).Get(ctx, oldPod.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, oldPod.DeletionTimestamp)
	oldPod.Finalizers = nil
	_, err = kube.CoreV1().Pods(oldPod.Namespace).Update(ctx, oldPod, metav1.UpdateOptions{})
	require.NoError(t, err)
	checkState(t, true)

	// A conflicting override after completion leaves the deployed spec and latch
	// intact, reports through the existing conditions, and clears after correction.
	beforeConflict, err := kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	setOverride(`{"controller":{"args":["--controllers=*"]}}`)
	checkConflict(true, autoApproverControllerName+"Degraded", certManagerControllerDeploymentControllerName+"Degraded")
	checkState(t, true)
	afterConflict, err := kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, beforeConflict.Spec, afterConflict.Spec)
	setOverride("")
	checkConflict(false, autoApproverControllerName+"Degraded", certManagerControllerDeploymentControllerName+"Degraded")

	// Existing SSA status writers must neither claim nor erase this typed field.
	require.NoError(t, opClient.ApplyOperatorStatus(ctx, "integration-status-writer", applyoperatorv1.OperatorStatus().WithConditions(
		applyoperatorv1.OperatorCondition().WithType("ExistingWriter").WithStatus(operatorv1.ConditionTrue),
	)))
	checkState(t, true)

	// The legacy UpdateStatus path can retry with a fresh resource version while
	// its informer still holds the pre-handoff object. It must preserve the latch.
	laggingInformers := operatorinformers.NewSharedInformerFactory(operator, 0)
	require.NoError(t, laggingInformers.Operator().V1alpha1().CertManagers().Informer().GetStore().Add(cm))
	laggingClient := &operatorclient.OperatorClient{Informers: laggingInformers, Client: operator.OperatorV1alpha1(), Clock: clock.RealClock{}}
	require.NoError(t, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		_, latestStatus, resourceVersion, err := laggingClient.GetOperatorStateWithQuorum(ctx)
		if err != nil {
			return err
		}
		v1helpers.SetOperatorCondition(&latestStatus.Conditions, operatorv1.OperatorCondition{Type: "LegacyWriter", Status: operatorv1.ConditionTrue})
		_, err = laggingClient.UpdateOperatorStatus(ctx, resourceVersion, latestStatus)
		return err
	}))
	checkState(t, true)
	policy.Status.Conditions[0].Status = metav1.ConditionFalse
	_, err = policyClient.UpdateStatus(ctx, policy, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Repair both forms of drift while readiness is false. Neither the original
	// static-resource loop nor an override may undo the established handoff.
	deployment, err := kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	deployment.Spec.Template.Spec.Containers[0].Args = common.MergeContainerArgs(deployment.Spec.Template.Spec.Containers[0].Args, []string{"--controllers=*"})
	_, err = kube.AppsV1().Deployments(operatorclient.TargetNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = kube.RbacV1().ClusterRoles().Create(ctx, resourceread.ReadClusterRoleV1OrDie(assets.MustAsset(autoApproverRoleAsset)), metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = kube.RbacV1().ClusterRoleBindings().Create(ctx, resourceread.ReadClusterRoleBindingV1OrDie(assets.MustAsset(autoApproverBindingAsset)), metav1.CreateOptions{})
	require.NoError(t, err)
	checkState(t, true)
	staticSync := factory.NewSyncContext("static-resources", recorder)
	t.Cleanup(staticSync.Queue().ShutDown)
	require.NoError(t, controllers[0].Sync(ctx, staticSync))
	checkState(t, true)

	require.NoError(t, policyClient.Delete(ctx, "cluster", metav1.DeleteOptions{}))
	policy, err = policyClient.Get(ctx, "cluster", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, policy.DeletionTimestamp)
	checkState(t, true)
	policy.Finalizers = nil
	_, err = policyClient.Update(ctx, policy, metav1.UpdateOptions{})
	require.NoError(t, err)
	checkState(t, false)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		eventList, err := kube.CoreV1().Events(metav1.NamespaceDefault).List(ctx, metav1.ListOptions{})
		require.NoError(c, err)
		var reasons []string
		for _, event := range eventList.Items {
			assert.Equal(c, "CertManager", event.InvolvedObject.Kind)
			assert.Equal(c, cm.UID, event.InvolvedObject.UID)
			assert.Equal(c, corev1.EventTypeNormal, event.Type)
			reasons = append(reasons, event.Reason)
		}
		assert.ElementsMatch(c, []string{"AutoApproverDisabled", "AutoApproverEnabled"}, reasons)
	}, 5*time.Second, 100*time.Millisecond)
}
