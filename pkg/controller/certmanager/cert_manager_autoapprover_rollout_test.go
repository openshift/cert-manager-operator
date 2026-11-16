package certmanager

import (
	"encoding/csv"
	"errors"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

func TestAutoApproverWaitsForDeploymentRollout(t *testing.T) {
	f := newAutoApproverTest(t)
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	f.assertDeploymentSelection("*,-certificaterequests-approver")

	// Accepting the new Deployment spec does not mean any new Pod is available.
	require.NoError(t, f.sync())
	f.assertState(false, false)
	f.assertTransitionEvents()
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(true, false)
	f.assertTransitionEvents("AutoApproverDisabled")
}

func TestAutoApproverRejectsConflictingOverride(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "before completion"
		if completed {
			name = "after completion"
		}
		t.Run(name, func(t *testing.T) {
			f := newAutoApproverTest(t)
			f.setPolicy(policyWithReady(metav1.ConditionTrue))
			if completed {
				require.NoError(t, f.sync())
				require.NoError(t, f.syncDeployment())
				f.completeRollout()
				require.NoError(t, f.sync())
			}
			cm := f.certManager()
			cm.Spec.UnsupportedConfigOverrides.Raw = []byte(`{"controller":{"args":["--controllers=*"]}}`)
			_, err := f.operator.OperatorV1alpha1().CertManagers().Update(t.Context(), cm, metav1.UpdateOptions{})
			require.NoError(t, err)
			before, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
			require.NoError(t, err)

			require.ErrorContains(t, f.sync(), "AutoApproverOverrideConflict")
			require.ErrorContains(t, f.syncDeployment(), "AutoApproverOverrideConflict")
			f.assertIntent(operatorv1.ConditionTrue)
			f.assertState(completed, false)
			assert.Equal(t, cm.Spec.UnsupportedConfigOverrides, f.certManager().Spec.UnsupportedConfigOverrides)
			after, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, before.Spec, after.Spec)

			// Removing the conflict resumes the same handoff without restoring RBAC.
			cm = f.certManager()
			cm.Spec.UnsupportedConfigOverrides.Raw = nil
			_, err = f.operator.OperatorV1alpha1().CertManagers().Update(t.Context(), cm, metav1.UpdateOptions{})
			require.NoError(t, err)
			require.NoError(t, f.sync())
			require.NoError(t, f.syncDeployment())
			f.completeRollout()
			require.NoError(t, f.sync())
			f.assertState(true, false)
			f.assertTransitionEvents("AutoApproverDisabled")

			// Once the policy CR is gone, the same override is allowed again.
			cm = f.certManager()
			cm.Spec.UnsupportedConfigOverrides.Raw = []byte(`{"controller":{"args":["--controllers=*"]}}`)
			_, err = f.operator.OperatorV1alpha1().CertManagers().Update(t.Context(), cm, metav1.UpdateOptions{})
			require.NoError(t, err)
			f.deletePolicy()
			require.NoError(t, f.sync())
			require.NoError(t, f.syncDeployment())
			require.NoError(t, f.sync())
			f.assertState(false, true)
			f.assertDeploymentSelection("*")
			f.assertTransitionEvents("AutoApproverDisabled", "AutoApproverEnabled")
		})
	}
}

func TestAutoApproverRejectsMalformedOverride(t *testing.T) {
	f := newAutoApproverTest(t)
	cm := f.certManager()
	cm.Spec.UnsupportedConfigOverrides.Raw = []byte(`{"controller":{"args":["--controllers=\"unterminated"]}}`)
	_, err := f.operator.OperatorV1alpha1().CertManagers().Update(t.Context(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Without an active handoff, keep the existing GA override behavior.
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	require.NoError(t, f.sync())
	f.assertState(false, true)
	f.assertDeploymentSelection(`"unterminated`)
	before, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)

	// Starting the handoff must report the parse error and block Deployment changes.
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	require.ErrorIs(t, f.sync(), csv.ErrQuote)
	require.ErrorIs(t, f.syncDeployment(), csv.ErrQuote)
	f.assertIntent(operatorv1.ConditionTrue)
	f.assertState(false, false)
	f.assertTransitionEvents()
	assert.Equal(t, cm.Spec.UnsupportedConfigOverrides, f.certManager().Spec.UnsupportedConfigOverrides)
	after, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, before.Spec, after.Spec)

	// A readiness change while blocked must not restore approval permissions.
	f.setPolicy(policyWithReady(metav1.ConditionFalse))
	require.ErrorIs(t, f.sync(), csv.ErrQuote)
	f.assertIntent(operatorv1.ConditionTrue)
	f.assertState(false, false)

	// Removing the invalid override lets the same handoff finish.
	cm = f.certManager()
	cm.Spec.UnsupportedConfigOverrides.Raw = nil
	_, err = f.operator.OperatorV1alpha1().CertManagers().Update(t.Context(), cm, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	require.NoError(t, f.sync())
	f.assertState(true, false)
	f.assertTransitionEvents("AutoApproverDisabled")
}

func TestAutoApproverEnabledCSVParsing(t *testing.T) {
	tests := []struct {
		name, selection string
		enabled         bool
		wantErr         error
	}{
		{name: "wildcard", selection: "*", enabled: true},
		{name: "quoted wildcard matches pflag/cert-manager", selection: `"*"`, enabled: true},
		{name: "csv-quoted field pair", selection: `"*","-certificaterequests-approver"`, enabled: false},
		{name: "fully quoted disable list is a single opaque name", selection: `"*,-certificaterequests-approver"`, enabled: false},
		{name: "explicit exclusion", selection: "*,-certificaterequests-approver", enabled: false},
		{name: "unterminated quote", selection: `"unterminated`, wantErr: csv.ErrQuote},
		{name: "bare quote", selection: `issu"ers`, wantErr: csv.ErrBareQuote},
		{name: "malformed field after exclusion", selection: `-certificaterequests-approver,"unterminated`, wantErr: csv.ErrQuote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, err := autoApproverEnabled(tt.selection)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.ErrorContains(t, err, "invalid --controllers value")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.enabled, enabled)
		})
	}
}

func TestAutoApproverControllerSelections(t *testing.T) {
	tests := []struct {
		name, selection string
		conflict        bool
	}{
		{name: "wildcard", selection: "*", conflict: true},
		{name: "quoted wildcard", selection: `"*"`, conflict: true},
		{name: "explicit approver", selection: "issuers,certificaterequests-approver", conflict: true},
		{name: "negative-only implicitly enables defaults", selection: "-ingress-shim", conflict: true},
		{name: "explicit exclusion", selection: "*,-certificaterequests-approver"},
		{name: "csv-quoted exclusion", selection: `"*","-certificaterequests-approver"`},
		{name: "negative-only excludes approver", selection: "-certificaterequests-approver"},
		{name: "exclusion wins regardless of order", selection: "-certificaterequests-approver,certificaterequests-approver,*"},
		{name: "allowlist without approver", selection: "issuers,certificates"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &v1alpha1.CertManager{}
			cm.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{OverrideArgs: []string{"--controllers=" + tt.selection}}
			selection, err := desiredAutoApproverSelection(cm, true)
			if tt.conflict {
				require.ErrorContains(t, err, "AutoApproverOverrideConflict")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.selection, selection)
			}
			// With no active handoff, existing GA override behavior is unchanged.
			selection, err = desiredAutoApproverSelection(cm, false)
			require.NoError(t, err)
			assert.Equal(t, tt.selection, selection)
		})
	}
	t.Run("effective unsupported selection takes precedence", func(t *testing.T) {
		cm := &v1alpha1.CertManager{}
		cm.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{OverrideArgs: []string{"--controllers=*"}}
		cm.Spec.UnsupportedConfigOverrides.Raw = []byte(`{"controller":{"args":["--controllers=issuers,certificates"]}}`)
		selection, err := desiredAutoApproverSelection(cm, true)
		require.NoError(t, err)
		assert.Equal(t, "issuers,certificates", selection)
	})
}

func TestAutoApproverIncompleteRolloutStatus(t *testing.T) {
	tests := []struct {
		name       string
		incomplete func(*appsv1.DeploymentStatus)
	}{
		{name: "generation not observed", incomplete: func(s *appsv1.DeploymentStatus) { s.ObservedGeneration-- }},
		{name: "replicas not updated", incomplete: func(s *appsv1.DeploymentStatus) { s.UpdatedReplicas = 0 }},
		{name: "replicas not available", incomplete: func(s *appsv1.DeploymentStatus) { s.AvailableReplicas = 0 }},
		{name: "old replicas remain", incomplete: func(s *appsv1.DeploymentStatus) { s.Replicas++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAutoApproverTest(t)
			f.setPolicy(policyWithReady(metav1.ConditionTrue))
			require.NoError(t, f.sync())
			require.NoError(t, f.syncDeployment())
			f.completeRollout()
			deployment, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
			require.NoError(t, err)
			tt.incomplete(&deployment.Status)
			_, err = f.kube.AppsV1().Deployments(deployment.Namespace).UpdateStatus(t.Context(), deployment, metav1.UpdateOptions{})
			require.NoError(t, err)

			require.NoError(t, f.sync())
			f.assertState(false, false)
			f.assertTransitionEvents()
			f.completeRollout()
			require.NoError(t, f.sync())
			f.assertState(true, false)
		})
	}
}

func TestAutoApproverReportsMalformedPodSelection(t *testing.T) {
	f := newAutoApproverTest(t)
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	deployment, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	old := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "malformed-controller", Namespace: deployment.Namespace, Labels: deployment.Spec.Selector.MatchLabels},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: deployment.Spec.Template.Spec.Containers[0].Name,
			Args: []string{`--controllers=-certificaterequests-approver,"unterminated`},
		}}},
	}
	_, err = f.kube.CoreV1().Pods(old.Namespace).Create(t.Context(), old, metav1.CreateOptions{})
	require.NoError(t, err)

	// Even a partially parsed exclusion must not allow handoff completion.
	err = f.sync()
	require.ErrorIs(t, err, csv.ErrQuote)
	assert.ErrorContains(t, err, "invalid --controllers value")
	assert.ErrorContains(t, err, "Pod "+old.Namespace+"/"+old.Name)
	f.assertState(false, false)
	f.assertIntent(operatorv1.ConditionTrue)
	f.assertTransitionEvents()

	require.NoError(t, f.kube.CoreV1().Pods(old.Namespace).Delete(t.Context(), old.Name, metav1.DeleteOptions{}))
	require.NoError(t, f.sync())
	f.assertState(true, false)
	f.assertTransitionEvents("AutoApproverDisabled")
}

func TestAutoApproverWaitsForQuotedWildcardPod(t *testing.T) {
	f := newAutoApproverTest(t)
	f.setPolicy(policyWithReady(metav1.ConditionTrue))
	require.NoError(t, f.sync())
	require.NoError(t, f.syncDeployment())
	f.completeRollout()
	deployment, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
	require.NoError(t, err)

	// A Pod whose args look "disabled" to naive Split parsing still enables the
	// approver under pflag CSV rules (`"*"` → `*`).
	old := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "quoted-wildcard", Namespace: deployment.Namespace, Labels: deployment.Spec.Selector.MatchLabels},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: deployment.Spec.Template.Spec.Containers[0].Name,
			Args: []string{`--controllers="*"`},
		}}},
	}
	_, err = f.kube.CoreV1().Pods(old.Namespace).Create(t.Context(), old, metav1.CreateOptions{})
	require.NoError(t, err)
	require.NoError(t, f.sync())
	f.assertState(false, false)
	f.assertTransitionEvents()

	require.NoError(t, f.kube.CoreV1().Pods(old.Namespace).Delete(t.Context(), old.Name, metav1.DeleteOptions{}))
	require.NoError(t, f.sync())
	f.assertState(true, false)
	f.assertTransitionEvents("AutoApproverDisabled")
}

func TestAutoApproverWaitsForOldPods(t *testing.T) {
	for _, terminating := range []bool{false, true} {
		name := "running old Pod"
		if terminating {
			name = "terminating old Pod with zero requested replicas"
		}
		t.Run(name, func(t *testing.T) {
			f := newAutoApproverTest(t)
			if terminating {
				cm := f.certManager()
				cm.Spec.ControllerConfig = &v1alpha1.DeploymentConfig{OverrideReplicas: ptr.To(int32(0))}
				_, err := f.operator.OperatorV1alpha1().CertManagers().Update(t.Context(), cm, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			f.setPolicy(policyWithReady(metav1.ConditionTrue))
			require.NoError(t, f.sync())
			require.NoError(t, f.syncDeployment())
			f.completeRollout()
			deployment, err := f.kube.AppsV1().Deployments(operatorclient.TargetNamespace).Get(t.Context(), certmanagerControllerDeployment, metav1.GetOptions{})
			require.NoError(t, err)
			old := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old-controller", Namespace: deployment.Namespace, Labels: deployment.Spec.Selector.MatchLabels},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: deployment.Spec.Template.Spec.Containers[0].Name}}}}
			if terminating {
				now := metav1.Now()
				old.DeletionTimestamp = &now
			}
			_, err = f.kube.CoreV1().Pods(old.Namespace).Create(t.Context(), old, metav1.CreateOptions{})
			require.NoError(t, err)
			require.NoError(t, f.sync())
			f.assertState(false, false)

			// A disabled current Pod and an unrelated enabled Pod must not block.
			current := old.DeepCopy()
			current.Name, current.DeletionTimestamp = "new-controller", nil
			current.Spec.Containers[0].Args = []string{"--controllers=*,-certificaterequests-approver"}
			current.Spec.Containers = append(current.Spec.Containers, corev1.Container{Name: "sidecar"})
			_, err = f.kube.CoreV1().Pods(current.Namespace).Create(t.Context(), current, metav1.CreateOptions{})
			require.NoError(t, err)
			unrelated := old.DeepCopy()
			unrelated.Name, unrelated.Labels = "unrelated", nil
			_, err = f.kube.CoreV1().Pods(unrelated.Namespace).Create(t.Context(), unrelated, metav1.CreateOptions{})
			require.NoError(t, err)
			require.NoError(t, f.kube.CoreV1().Pods(old.Namespace).Delete(t.Context(), old.Name, metav1.DeleteOptions{}))
			require.NoError(t, f.sync())
			f.assertState(true, false)
		})
	}
}

func TestAutoApproverCompletionReadFailures(t *testing.T) {
	t.Run("Pod list failure leaves completion unconfirmed", func(t *testing.T) {
		f := newAutoApproverTest(t)
		f.setPolicy(policyWithReady(metav1.ConditionTrue))
		require.NoError(t, f.sync())
		require.NoError(t, f.syncDeployment())
		f.completeRollout()
		f.kube.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("cannot observe Pods")
		})
		require.ErrorContains(t, f.sync(), "cannot observe Pods")
		f.assertState(false, false)
		f.assertIntent(operatorv1.ConditionTrue)
		f.assertTransitionEvents()
	})
	t.Run("RBAC reappears during rollout observation", func(t *testing.T) {
		f := newAutoApproverTest(t)
		f.setPolicy(policyWithReady(metav1.ConditionTrue))
		require.NoError(t, f.sync())
		require.NoError(t, f.syncDeployment())
		f.completeRollout()
		f.kube.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
			require.NoError(t, f.kube.Tracker().Add(resourceread.ReadClusterRoleBindingV1OrDie(assets.MustAsset(autoApproverBindingAsset))))
			return false, nil, nil
		})
		require.ErrorContains(t, f.sync(), "waiting for auto-approver ClusterRoleBinding deletion")
		assert.False(t, f.certManager().Status.AutoApproverDisabled)
		f.assertIntent(operatorv1.ConditionTrue)
		f.assertTransitionEvents()
	})
}
