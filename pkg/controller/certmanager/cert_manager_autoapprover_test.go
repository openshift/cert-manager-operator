package certmanager

import (
	"errors"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	operatorfake "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"
	operatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
	"github.com/openshift/cert-manager-operator/pkg/operator/utils"
)

func TestAutoApproverState(t *testing.T) {
	tests := []struct {
		name          string
		policy        *v1alpha1.ApproverPolicyManager
		latched       bool
		intent        operatorv1.ConditionStatus
		shouldDisable bool
	}{
		{name: "no policy leaves approval enabled"},
		{name: "policy deletion releases the latch", latched: true},
		{name: "policy deletion releases incomplete handoff", intent: operatorv1.ConditionTrue},
		{name: "missing Ready keeps approval enabled", policy: &v1alpha1.ApproverPolicyManager{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}},
		{name: "Ready False keeps approval enabled", policy: policyWithReady(metav1.ConditionFalse)},
		{name: "Ready Unknown keeps approval enabled", policy: policyWithReady(metav1.ConditionUnknown)},
		{name: "Ready True starts handoff", policy: policyWithReady(metav1.ConditionTrue), shouldDisable: true},
		{name: "readiness failure holds latch", policy: policyWithReady(metav1.ConditionFalse), latched: true, shouldDisable: true},
		{name: "intent holds incomplete handoff", policy: policyWithReady(metav1.ConditionFalse), intent: operatorv1.ConditionTrue, shouldDisable: true},
		{name: "explicit release overrides pending completion", policy: policyWithReady(metav1.ConditionFalse), intent: operatorv1.ConditionFalse, latched: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &v1alpha1.CertManager{}
			cm.Status.AutoApproverDisabled = tt.latched
			if tt.intent != "" {
				cm.Status.Conditions = []operatorv1.OperatorCondition{{Type: autoApproverDisableRequested, Status: tt.intent}}
			}
			shouldDisable := autoApproverState(cm, tt.policy)
			assert.Equal(t, tt.shouldDisable, shouldDisable)
		})
	}
	t.Run("Ready for a previous generation cannot start handoff", func(t *testing.T) {
		policy := policyWithReady(metav1.ConditionTrue)
		policy.Generation++
		shouldDisable := autoApproverState(&v1alpha1.CertManager{}, policy)
		assert.False(t, shouldDisable)
	})
}

func TestAutoApproverLifecycle(t *testing.T) {
	f := newAutoApproverTest(t)

	// Ordinary GA reconciliation creates the original approval RBAC.
	require.NoError(t, f.sync())
	f.assertState(false, true)
	f.assertTransitionEvents()

	// Creating a policy CR alone must not turn off built-in approval.
	f.setPolicy(policyWithReady(metav1.ConditionFalse))
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(false, true)

	// RBAC is removed immediately, but status waits for the Deployment write.
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	require.NoError(t, f.sync())
	f.assertState(false, false)
	f.assertIntent(operatorv1.ConditionTrue)
	f.assertTransitionEvents()
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(true, false)
	f.assertTransitionEvents("AutoApproverDisabled")

	// Repeated reconciliation and a later readiness failure hold the latch.
	f.setPolicy(policyWithReady(metav1.ConditionFalse))
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(true, false)
	f.assertTransitionEvents("AutoApproverDisabled")

	// DeletionTimestamp is insufficient: finalizer cleanup must finish first.
	policy := policyWithReady(metav1.ConditionFalse)
	now := metav1.Now()
	policy.DeletionTimestamp = &now
	policy.Finalizers = []string{"approver-policy.operator.openshift.io/finalizer"}
	f.setPolicy(policy)
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(true, false)

	// Only NotFound releases the latch, after both RBAC and args are restored.
	f.deletePolicy()
	require.NoError(t, f.sync())
	f.assertState(true, true)
	f.assertIntent(operatorv1.ConditionFalse)
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(false, true)
	require.NoError(t, f.sync())
	f.assertTransitionEvents("AutoApproverDisabled", "AutoApproverEnabled")
}

func TestAutoApproverWaitsForMissingDeployment(t *testing.T) {
	tests := []struct {
		name   string
		policy *v1alpha1.ApproverPolicyManager
	}{
		{name: "initial installation"},
		{name: "active handoff", policy: policyWithReady(metav1.ConditionTrue)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAutoApproverTest(t)
			if tt.policy != nil {
				f.setPolicy(tt.policy)
			}
			require.NoError(t, f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Delete(t.Context(), certmanagerControllerDeployment, metav1.DeleteOptions{}))

			// Missing the Deployment is a waiting state, not a completed handoff.
			require.NoError(t, f.sync())
			f.assertState(false, tt.policy == nil)
			f.assertTransitionEvents()
			if tt.policy != nil {
				f.assertIntent(operatorv1.ConditionTrue)
			}

			// A retry is queued even if no Deployment informer event arrives.
			require.Eventually(t, func() bool { return f.syncCtx.Queue().Len() == 1 }, 5*time.Second, 10*time.Millisecond)
			key, shutdown := f.syncCtx.Queue().Get()
			require.False(t, shutdown)
			assert.Equal(t, f.syncCtx.QueueKey(), key)
			f.syncCtx.Queue().Done(key)

			// Once the Deployment is created and ready, reconciliation can finish.
			require.NoError(t, f.syncDeployment())
			f.completeRollout()
			require.NoError(t, f.sync())
			f.assertState(tt.policy != nil, tt.policy == nil)
			if tt.policy != nil {
				f.assertTransitionEvents("AutoApproverDisabled")
			} else {
				f.assertTransitionEvents()
			}
		})
	}
}

func TestAutoApproverReturnsDeploymentReadErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "forbidden", err: apierrors.NewForbidden(appsv1.Resource("deployments"), certmanagerControllerDeployment, errors.New("access denied"))},
		{name: "timeout", err: apierrors.NewTimeoutError("Deployment read timed out", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAutoApproverTest(t)
			f.kube.PrependReactor("get", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
				return true, nil, tt.err
			})
			require.ErrorIs(t, f.sync(), tt.err)
			f.assertState(false, true)
			f.assertTransitionEvents()
		})
	}
}

func TestAutoApproverReadinessFlickerBeforeCompletion(t *testing.T) {
	f := newAutoApproverTest(t)
	require.NoError(t, f.sync())
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	require.NoError(t, f.sync())
	f.assertState(false, false) // RBAC is gone, but the Deployment has not updated yet.
	f.assertIntent(operatorv1.ConditionTrue)

	f.setPolicy(policyWithReady(metav1.ConditionFalse))
	require.NoError(t, f.sync())
	f.assertState(false, false) // A readiness flicker must not restore approval RBAC.
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	f.assertDeploymentSelection("*,-certificaterequests-approver")
	require.NoError(t, f.sync())
	f.assertState(true, false)
	f.assertTransitionEvents("AutoApproverDisabled")
}

func TestAutoApproverIntentMustPersistBeforeHandoff(t *testing.T) {
	f := newAutoApproverTest(t)
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	f.setPolicy(policyWithReady(metav1.ConditionTrue))

	fail := true
	f.operator.PrependReactor("update", "certmanagers", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return fail && action.GetSubresource() == "status", nil, errors.New("cannot persist intent")
	})
	f.kube.ClearActions()
	require.ErrorContains(t, f.sync(), "cannot persist intent")
	assert.Empty(t, f.kube.Actions())
	f.assertState(false, true)
	assert.Nil(t, v1helpers.FindOperatorCondition(f.certManager().Status.Conditions, autoApproverDisableRequested))

	// The independent Deployment controller must also wait for durable intent.
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	f.assertDeploymentSelection("*")
	f.assertTransitionEvents()

	fail = false
	f.kube.PrependReactor("delete", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
		f.assertIntent(operatorv1.ConditionTrue)
		assert.False(t, f.certManager().Status.AutoApproverDisabled)
		return false, nil, nil
	})
	require.NoError(t, f.sync())
	f.assertState(false, false)

	// Retrying while waiting for the Deployment must not rewrite the intent.
	f.operator.ClearActions()
	require.NoError(t, f.sync())
	for _, action := range f.operator.Actions() {
		assert.NotEqual(t, "update", action.GetVerb())
	}
}

func TestAutoApproverRetainsIntentUntilDeletionIsConfirmed(t *testing.T) {
	f := newAutoApproverTest(t)
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	require.NoError(t, f.sync())
	f.assertState(false, false)

	// An empty informer cache is not evidence of deletion. The API still has the CR.
	require.NoError(t, f.informers.Operator().V1alpha1().ApproverPolicyManagers().Informer().GetStore().Delete(policyWithReady(metav1.ConditionTrue)))
	require.NoError(t, f.sync())
	f.assertIntent(operatorv1.ConditionTrue)
	f.assertState(false, false)

	f.deletePolicy()
	fail := true
	f.operator.PrependReactor("get", "approverpolicymanagers", func(clienttesting.Action) (bool, runtime.Object, error) {
		return fail, nil, apierrors.NewTimeoutError("cannot confirm policy deletion", 1)
	})
	f.kube.ClearActions()
	require.ErrorContains(t, f.sync(), "cannot confirm policy deletion")
	assert.Empty(t, f.kube.Actions())
	f.assertIntent(operatorv1.ConditionTrue)
	f.assertState(false, false)
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	f.assertDeploymentSelection("*,-certificaterequests-approver")

	fail = false
	require.NoError(t, f.sync())
	f.assertIntent(operatorv1.ConditionFalse)
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(false, true)
	f.assertDeploymentSelection("*")
}

func TestAutoApproverFailedIntentClearDoesNotRestoreApproval(t *testing.T) {
	f := newAutoApproverTest(t)
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(true, false)
	f.deletePolicy()

	fail := true
	f.operator.PrependReactor("update", "certmanagers", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return fail && action.GetSubresource() == "status", nil, errors.New("cannot clear intent")
	})
	f.kube.ClearActions()
	require.ErrorContains(t, f.sync(), "cannot clear intent")
	assert.Empty(t, f.kube.Actions())
	f.assertIntent(operatorv1.ConditionTrue)
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	f.assertDeploymentSelection("*,-certificaterequests-approver")
	f.assertState(true, false)

	fail = false
	require.NoError(t, f.sync())
	f.assertIntent(operatorv1.ConditionFalse)
	f.assertState(true, true) // Completion remains true until args are restored.
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(false, true)
	f.assertTransitionEvents("AutoApproverDisabled", "AutoApproverEnabled")
}

func TestAutoApproverMigratesCompletedHandoffToIntent(t *testing.T) {
	f := newAutoApproverTest(t)
	cm := f.certManager()
	cm.Status.AutoApproverDisabled = true
	_, err := f.operator.OperatorV1alpha1().CertManagers().UpdateStatus(t.Context(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	f.setPolicy(policyWithReady(metav1.ConditionFalse))

	// Before the coordinator runs, the Deployment hook must honor the old latch.
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	f.assertDeploymentSelection("*,-certificaterequests-approver")
	require.NoError(t, f.sync())
	f.assertIntent(operatorv1.ConditionTrue)
	f.assertState(true, false)
	f.assertTransitionEvents()
}

func TestAutoApproverPartialFailures(t *testing.T) {
	for _, resource := range []string{"clusterroles", "clusterrolebindings"} {
		t.Run("retry failed deletion of "+resource, func(t *testing.T) {
			f := newAutoApproverTest(t)
			require.NoError(t, f.sync())
			f.setPolicy(policyWithReady(metav1.ConditionTrue))
			fail := true
			f.kube.PrependReactor("delete", resource, func(clienttesting.Action) (bool, runtime.Object, error) {
				return fail, nil, errors.New("injected delete failure")
			})
			require.ErrorContains(t, f.sync(), "injected delete failure")
			require.ErrorContains(t, f.syncDeployment(), "waiting for auto-approver")
			f.assertDeploymentSelection("*")
			assert.False(t, f.certManager().Status.AutoApproverDisabled)
			f.assertIntent(operatorv1.ConditionTrue)
			f.assertTransitionEvents()
			f.setPolicy(policyWithReady(metav1.ConditionFalse))
			fail = false
			require.NoError(t, f.sync())
			require.NoError(t, f.syncDeployment())
			f.completeRollout()
			require.NoError(t, f.sync())
			f.assertState(true, false)
			f.assertTransitionEvents("AutoApproverDisabled")
		})
	}
	t.Run("RBAC guard remains effective when deployment update fails", func(t *testing.T) {
		f := newAutoApproverTest(t)
		require.NoError(t, f.sync())
		f.setPolicy(policyWithReady(metav1.ConditionTrue))
		require.NoError(t, f.sync())
		fail := true
		f.kube.PrependReactor("update", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) {
			return fail, nil, errors.New("injected deployment failure")
		})
		require.ErrorContains(t, f.syncDeployment(), "injected deployment failure")
		f.setPolicy(policyWithReady(metav1.ConditionFalse))
		require.NoError(t, f.sync())
		f.assertState(false, false)
		f.assertIntent(operatorv1.ConditionTrue)
		f.assertTransitionEvents()
		fail = false
		require.NoError(t, f.syncDeployment())
		f.completeRollout()
		require.NoError(t, f.sync())
		f.assertState(true, false)
	})
	t.Run("failed completion write retains intent across readiness flicker and restart", func(t *testing.T) {
		f := newAutoApproverTest(t)
		f.setPolicy(policyWithReady(metav1.ConditionTrue))
		require.NoError(t, f.sync())
		require.NoError(t, f.syncDeployment())
		f.completeRollout()
		fail := true
		f.operator.PrependReactor("update", "certmanagers", func(action clienttesting.Action) (bool, runtime.Object, error) {
			return fail && action.GetSubresource() == "status", nil, errors.New("injected status conflict")
		})
		require.ErrorContains(t, f.sync(), "injected status conflict")
		f.assertState(false, false)
		f.assertIntent(operatorv1.ConditionTrue)
		f.assertTransitionEvents()
		f.setPolicy(policyWithReady(metav1.ConditionFalse))
		// Reconstruct the controller and its cache using only persisted objects.
		f.controller = &CertManagerAutoApproverController{configClient: f.operator.OperatorV1alpha1(), kubeClient: f.kube}
		f.informers = operatorinformers.NewSharedInformerFactory(f.operator, 0)
		require.NoError(t, f.syncDeployment())
		f.completeRollout()
		f.assertDeploymentSelection("*,-certificaterequests-approver")
		require.ErrorContains(t, f.sync(), "injected status conflict")
		f.assertState(false, false)
		fail = false
		require.NoError(t, f.sync())
		f.assertState(true, false)
		f.assertTransitionEvents("AutoApproverDisabled")
	})
}

func TestAutoApproverRespectsManagementState(t *testing.T) {
	for _, state := range []operatorv1.ManagementState{operatorv1.Unmanaged, operatorv1.Removed} {
		t.Run(string(state), func(t *testing.T) {
			f := newAutoApproverTest(t)
			cm := f.certManager()
			cm.Spec.ManagementState = state
			_, err := f.operator.OperatorV1alpha1().CertManagers().Update(t.Context(), cm, metav1.UpdateOptions{})
			require.NoError(t, err)
			f.setPolicy(policyWithReady(metav1.ConditionTrue))
			f.kube.ClearActions()
			require.NoError(t, f.sync())
			assert.Empty(t, f.kube.Actions())
			assert.False(t, f.certManager().Status.AutoApproverDisabled)
		})
	}
}

func TestAutoApproverWaitsForRBACDeletion(t *testing.T) {
	f := newAutoApproverTest(t)
	require.NoError(t, f.sync())
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	// Model an accepted DELETE whose object remains because of a finalizer.
	held := true
	f.kube.PrependReactor("delete", "clusterroles", func(clienttesting.Action) (bool, runtime.Object, error) {
		return held, nil, nil
	})
	require.ErrorContains(t, f.sync(), "waiting for auto-approver ClusterRole deletion")
	require.ErrorContains(t, f.syncDeployment(), "waiting for auto-approver ClusterRole deletion")
	f.assertDeploymentSelection("*")
	assert.False(t, f.certManager().Status.AutoApproverDisabled)
	f.assertTransitionEvents()
	held = false
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(true, false)
}

func TestAutoApproverRetriesRBACRestoration(t *testing.T) {
	for _, resource := range []string{"clusterroles", "clusterrolebindings"} {
		t.Run(resource, func(t *testing.T) {
			f := newAutoApproverTest(t)
			f.setPolicy(policyWithReady(metav1.ConditionTrue))
			require.NoError(t, f.sync())
			require.NoError(t, f.syncDeployment())
			f.completeRollout()
			require.NoError(t, f.sync())
			f.assertState(true, false)
			f.deletePolicy()
			fail := true
			f.kube.PrependReactor("create", resource, func(clienttesting.Action) (bool, runtime.Object, error) {
				return fail, nil, errors.New("injected restoration failure")
			})
			require.ErrorContains(t, f.sync(), "injected restoration failure")
			assert.True(t, f.certManager().Status.AutoApproverDisabled)
			f.assertTransitionEvents("AutoApproverDisabled")
			fail = false
			require.NoError(t, f.sync())
			require.NoError(t, f.syncDeployment())
			f.completeRollout()
			require.NoError(t, f.sync())
			f.assertState(false, true)
			f.assertTransitionEvents("AutoApproverDisabled", "AutoApproverEnabled")
		})
	}
}

func TestAutoApproverPreservesCompatibleOverrides(t *testing.T) {
	f := newAutoApproverTest(t)
	cm := f.certManager()
	cm.Spec.UnsupportedConfigOverrides.Raw = []byte(`{"controller":{"args":["--controllers=issuers,certificates","--tls-min-version=VersionTLS13"]}}`)
	_, err := f.operator.OperatorV1alpha1().CertManagers().Update(t.Context(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(true, false)
	deployment, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	selection, err := approverControllerSelection(deployment.Spec.Template.Spec.Containers[0].Args, false)
	require.NoError(t, err)
	assert.Equal(t, "issuers,certificates", selection)
	assert.Contains(t, deployment.Spec.Template.Spec.Containers[0].Args, "--tls-min-version=VersionTLS13")
	assert.Equal(t, cm.Spec.UnsupportedConfigOverrides, f.certManager().Spec.UnsupportedConfigOverrides)

	f.deletePolicy()
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(false, true)
	deployment, err = f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	selection, err = approverControllerSelection(deployment.Spec.Template.Spec.Containers[0].Args, false)
	require.NoError(t, err)
	assert.Equal(t, "issuers,certificates", selection)
}

type autoApproverTest struct {
	t          *testing.T
	controller *CertManagerAutoApproverController
	operator   *operatorfake.Clientset
	kube       *kubefake.Clientset
	informers  operatorinformers.SharedInformerFactory
	syncCtx    factory.SyncContext
}

func newAutoApproverTest(t *testing.T) *autoApproverTest {
	t.Helper()
	cm := &v1alpha1.CertManager{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", UID: "cert-manager-test"},
		Spec:       v1alpha1.CertManagerSpec{OperatorSpec: operatorv1.OperatorSpec{ManagementState: operatorv1.Managed}},
		Status: v1alpha1.CertManagerStatus{OperatorStatus: operatorv1.OperatorStatus{
			ObservedGeneration: 7,
			Conditions:         []operatorv1.OperatorCondition{{Type: "ExistingCondition", Status: operatorv1.ConditionTrue}},
		}},
	}
	operator := operatorfake.NewSimpleClientset(cm)
	operatorInformers := operatorinformers.NewSharedInformerFactory(operator, 0)
	deployment := resourceread.ReadDeploymentV1OrDie(assets.MustAsset(certManagerControllerDeploymentFile))
	deployment.Generation = 1
	kube := kubefake.NewSimpleClientset(deployment)
	syncCtx := factory.NewSyncContext("test-auto-approver", events.NewInMemoryRecorder("test", clock.RealClock{}))
	t.Cleanup(syncCtx.Queue().ShutDown)
	return &autoApproverTest{t: t, operator: operator, kube: kube, informers: operatorInformers, syncCtx: syncCtx,
		controller: &CertManagerAutoApproverController{configClient: operator.OperatorV1alpha1(), kubeClient: kube},
	}
}

func policyWithReady(ready metav1.ConditionStatus) *v1alpha1.ApproverPolicyManager {
	return &v1alpha1.ApproverPolicyManager{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster", Generation: 1},
		Status: v1alpha1.ApproverPolicyManagerStatus{ConditionalStatus: v1alpha1.ConditionalStatus{
			Conditions: []metav1.Condition{{Type: v1alpha1.Ready, Status: ready, ObservedGeneration: 1}},
		}},
	}
}

func (f *autoApproverTest) setPolicy(policy *v1alpha1.ApproverPolicyManager) {
	f.t.Helper()
	err := f.operator.Tracker().Add(policy)
	if apierrors.IsAlreadyExists(err) {
		err = f.operator.Tracker().Update(v1alpha1.SchemeGroupVersion.WithResource("approverpolicymanagers"), policy, "")
	}
	require.NoError(f.t, err)
	require.NoError(f.t, f.informers.Operator().V1alpha1().ApproverPolicyManagers().Informer().GetStore().Update(policy))
}

func (f *autoApproverTest) deletePolicy() {
	f.t.Helper()
	require.NoError(f.t, f.operator.OperatorV1alpha1().ApproverPolicyManagers().Delete(f.t.Context(), "cluster", metav1.DeleteOptions{}))
	require.NoError(f.t, f.informers.Operator().V1alpha1().ApproverPolicyManagers().Informer().GetStore().Delete(policyWithReady(metav1.ConditionTrue)))
}

func (f *autoApproverTest) certManager() *v1alpha1.CertManager {
	f.t.Helper()
	cm, err := f.operator.OperatorV1alpha1().CertManagers().Get(f.t.Context(), "cluster", metav1.GetOptions{})
	require.NoError(f.t, err)
	return cm
}

func (f *autoApproverTest) sync() error {
	return f.controller.sync(f.t.Context(), f.syncCtx)
}

// Exercise the actual library-go Deployment apply path, including all existing
// hooks. Refresh the fake informer explicitly; no informer goroutines run here.
func (f *autoApproverTest) syncDeployment() error {
	f.t.Helper()
	cm := f.certManager()
	require.NoError(f.t, f.informers.Operator().V1alpha1().CertManagers().Informer().GetStore().Update(cm))
	operatorClient := v1helpers.NewFakeOperatorClientWithObjectMeta(&cm.ObjectMeta, &cm.Spec.OperatorSpec, &cm.Status.OperatorStatus, nil)
	controller := NewCertManagerControllerDeploymentController(operatorClient, f.informers, utils.OptionalInformer[configinformers.SharedInformerFactory]{}, f.kube, informers.NewSharedInformerFactory(f.kube, 0), f.syncCtx.Recorder(), "test", status.NewVersionGetter(), "", "")
	return controller.Sync(f.t.Context(), f.syncCtx)
}

// Fake clients do not run Kubernetes' Deployment controller. Tests explicitly
// report rollout completion after applying the desired Deployment.
func (f *autoApproverTest) completeRollout() {
	f.t.Helper()
	deployment, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(f.t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(f.t, err)
	replicas := ptr.Deref(deployment.Spec.Replicas, 1)
	deployment.Status = appsv1.DeploymentStatus{
		ObservedGeneration: deployment.Generation, Replicas: replicas,
		UpdatedReplicas: replicas, ReadyReplicas: replicas, AvailableReplicas: replicas,
	}
	_, err = f.kube.AppsV1().Deployments(deployment.Namespace).UpdateStatus(f.t.Context(), deployment, metav1.UpdateOptions{})
	require.NoError(f.t, err)
}

func (f *autoApproverTest) assertState(disabled, rbacPresent bool) {
	f.t.Helper()
	cm := f.certManager()
	assert.Equal(f.t, disabled, cm.Status.AutoApproverDisabled)
	assert.EqualValues(f.t, 7, cm.Status.ObservedGeneration)
	assert.Equal(f.t, "ExistingCondition", cm.Status.Conditions[0].Type)
	_, roleErr := f.kube.RbacV1().ClusterRoles().Get(f.t.Context(), autoApproverRBACName, metav1.GetOptions{})
	_, bindingErr := f.kube.RbacV1().ClusterRoleBindings().Get(f.t.Context(), autoApproverRBACName, metav1.GetOptions{})
	if rbacPresent {
		require.NoError(f.t, roleErr)
		require.NoError(f.t, bindingErr)
	} else {
		assert.True(f.t, apierrors.IsNotFound(roleErr))
		assert.True(f.t, apierrors.IsNotFound(bindingErr))
	}
}

func (f *autoApproverTest) assertIntent(expected operatorv1.ConditionStatus) {
	f.t.Helper()
	condition := v1helpers.FindOperatorCondition(f.certManager().Status.Conditions, autoApproverDisableRequested)
	require.NotNil(f.t, condition)
	assert.Equal(f.t, expected, condition.Status)
	assert.False(f.t, condition.LastTransitionTime.IsZero())
}

func (f *autoApproverTest) assertDeploymentSelection(expected string) {
	f.t.Helper()
	deployment, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(f.t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(f.t, err)
	selection, err := approverControllerSelection(deployment.Spec.Template.Spec.Containers[0].Args, false)
	require.NoError(f.t, err)
	assert.Equal(f.t, expected, selection)
}

func (f *autoApproverTest) assertTransitionEvents(reasons ...string) {
	f.t.Helper()
	events, err := f.kube.CoreV1().Events("").List(f.t.Context(), metav1.ListOptions{})
	require.NoError(f.t, err)
	actual := make([]string, 0, len(events.Items))
	for _, event := range events.Items {
		assert.Equal(f.t, corev1.EventTypeNormal, event.Type)
		assert.Equal(f.t, "CertManager", event.InvolvedObject.Kind)
		assert.Equal(f.t, "cluster", event.InvolvedObject.Name)
		assert.Equal(f.t, f.certManager().UID, event.InvolvedObject.UID)
		actual = append(actual, event.Reason)
	}
	assert.ElementsMatch(f.t, reasons, actual)
}
