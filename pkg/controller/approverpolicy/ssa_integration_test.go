package approverpolicy

import (
	"context"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

// newImpersonatedOperatorReconciler builds a Reconciler whose every read and
// write runs as the operator's real ServiceAccount permissions, decoded
// straight from config/rbac/role.yaml, including Kubernetes' RBAC escalation
// checks when creating the operand's ClusterRole. That real-RBAC requirement
// is why this test proxies the fake CtrlClient to an impersonated client
// instead of recording calls in memory like the rest of the package's tests.
func newImpersonatedOperatorReconciler(t *testing.T, config *rest.Config, scheme *runtime.Scheme) (*Reconciler, *fakes.FakeCtrlClient, client.Client) {
	t.Helper()
	live, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	require.NoError(t, live.Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operandNamespace}}))

	roleBytes, err := os.ReadFile("../../../config/rbac/role.yaml")
	require.NoError(t, err)
	decoded, _, err := codecs.UniversalDeserializer().Decode(roleBytes, nil, nil)
	require.NoError(t, err)
	operatorRole := decoded.(*rbacv1.ClusterRole)
	require.NoError(t, live.Create(t.Context(), operatorRole))
	require.NoError(t, live.Create(t.Context(), &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "operator-test"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: operatorRole.Name},
		Subjects:   []rbacv1.Subject{{Kind: "User", APIGroup: rbacv1.GroupName, Name: "approver-policy-operator-test"}},
	}))

	operatorConfig := rest.CopyConfig(config)
	operatorConfig.Impersonate.UserName = "approver-policy-operator-test"
	operatorClient, err := client.New(operatorConfig, client.Options{Scheme: scheme})
	require.NoError(t, err)

	c := &fakes.FakeCtrlClient{}
	c.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		return operatorClient.Get(ctx, key, obj)
	})
	c.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
		return operatorClient.Patch(ctx, obj, patch, opts...)
	})
	return &Reconciler{CtrlClient: c, apiReader: operatorClient, log: logr.Discard()}, c, live
}

// TestServerSideApply exercises reconcileApproverPolicy against a real API
// server (envtest) because the unit fake cannot model SSA field ownership,
// RBAC escalation checks, or server defaulting. Run with KUBEBUILDER_ASSETS
// pointing at envtest binaries. The subtests below share one envtest instance
// and a steadily-evolving set of live objects, so they must run in order.
func TestServerSideApply(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("set KUBEBUILDER_ASSETS to run SSA integration")
	}
	t.Setenv(imageEnvVar, "registry.example/approver:test")

	env := &envtest.Environment{}
	config, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, clientscheme.AddToScheme(scheme))

	r, c, live := newImpersonatedOperatorReconciler(t, config, scheme)
	instance := testInstance()
	instance.Spec.ControllerConfig.Labels = map[string]string{"example.test/remove": "old"}

	t.Run("install applies every resource and waits for the rollout", func(t *testing.T) {
		require.True(t, common.IsRetryRequiredError(r.reconcileApproverPolicy(t.Context(), instance)))
		assert.Equal(t, 10, c.PatchCallCount())
	})

	t.Run("a repeated reconcile is a no-op once applied", func(t *testing.T) {
		patches := c.PatchCallCount()
		require.True(t, common.IsRetryRequiredError(r.reconcileApproverPolicy(t.Context(), instance)))
		assert.Equal(t, patches, c.PatchCallCount(), "server defaults must not cause a second apply")
	})

	var secret *corev1.Secret
	var webhook *admissionregistrationv1.ValidatingWebhookConfiguration
	t.Run("fields owned by other writers survive reconciliation", func(t *testing.T) {
		secret = &corev1.Secret{}
		require.NoError(t, live.Get(t.Context(), client.ObjectKey{Namespace: operandNamespace, Name: tlsSecretName}, secret))
		secret.Data = map[string][]byte{"tls.crt": []byte("generated-cert"), "tls.key": []byte("generated-key")}
		secret.Annotations["external.example/keep"] = "value"
		require.NoError(t, live.Update(t.Context(), secret, client.FieldOwner("operand")))

		webhook = &admissionregistrationv1.ValidatingWebhookConfiguration{}
		require.NoError(t, live.Get(t.Context(), client.ObjectKey{Name: operandName}, webhook))
		webhook.Webhooks[0].ClientConfig.CABundle = []byte("injected-ca")
		require.NoError(t, live.Update(t.Context(), webhook, client.FieldOwner("cainjector")))

		instance.Spec.ControllerConfig.Labels = nil
		instance.Spec.ControllerConfig.Annotations = map[string]string{"example.test/change": "new"}
		require.True(t, common.IsRetryRequiredError(r.reconcileApproverPolicy(t.Context(), instance)))

		require.NoError(t, live.Get(t.Context(), client.ObjectKeyFromObject(secret), secret))
		assert.Equal(t, []byte("generated-key"), secret.Data["tls.key"], "the operand's own TLS material is not ours to overwrite")
		assert.Equal(t, "value", secret.Annotations["external.example/keep"])
		assert.NotContains(t, secret.Labels, "example.test/remove", "a dropped operator-configured label is still removed")

		require.NoError(t, live.Get(t.Context(), client.ObjectKeyFromObject(webhook), webhook))
		assert.Equal(t, []byte("injected-ca"), webhook.Webhooks[0].ClientConfig.CABundle, "cert-manager's CA injector owns this field")
	})

	var deployment *appsv1.Deployment
	t.Run("signer scope changes update the ClusterRole without rolling the Deployment", func(t *testing.T) {
		deployment = &appsv1.Deployment{}
		require.NoError(t, live.Get(t.Context(), client.ObjectKey{Namespace: operandNamespace, Name: operandName}, deployment))
		generation := deployment.Generation

		for _, names := range [][]string{{"external.example/*"}, {"issuers.cert-manager.io/*"}, nil} {
			instance.Spec.ApproverPolicyConfig.ApproveSignerNames = names
			require.True(t, common.IsRetryRequiredError(r.reconcileApproverPolicy(t.Context(), instance)))

			role := &rbacv1.ClusterRole{}
			require.NoError(t, live.Get(t.Context(), client.ObjectKey{Name: operandName}, role))
			for _, rule := range role.Rules {
				if len(rule.Resources) == 1 && rule.Resources[0] == "signers" {
					assert.ElementsMatch(t, names, rule.ResourceNames)
				}
			}

			require.NoError(t, live.Get(t.Context(), client.ObjectKeyFromObject(deployment), deployment))
			assert.Equal(t, generation, deployment.Generation, "a signer-scope change is not a Deployment rollout")
		}
	})

	t.Run("reconcile succeeds once the rollout is observed ready", func(t *testing.T) {
		deployment.Status = availableDeploymentStatus(deployment.Generation)
		require.NoError(t, live.Status().Update(t.Context(), deployment))
		require.NoError(t, r.reconcileApproverPolicy(t.Context(), instance))
	})

	t.Run("a managed field removed by an external writer is restored", func(t *testing.T) {
		require.NoError(t, live.Get(t.Context(), client.ObjectKeyFromObject(secret), secret))
		delete(secret.Labels, "app")
		require.NoError(t, live.Update(t.Context(), secret, client.FieldOwner("external")))

		require.NoError(t, r.reconcileApproverPolicy(t.Context(), instance))

		restored := &corev1.Secret{}
		require.NoError(t, live.Get(t.Context(), client.ObjectKeyFromObject(secret), restored))
		assert.Equal(t, operandName, restored.Labels["app"])
	})

	t.Run("an unowned resource is never silently adopted", func(t *testing.T) {
		orphaned := &corev1.Secret{}
		require.NoError(t, live.Get(t.Context(), client.ObjectKeyFromObject(secret), orphaned))
		orphaned.OwnerReferences = nil
		require.NoError(t, live.Update(t.Context(), orphaned, client.FieldOwner("external")))

		require.True(t, common.IsIrrecoverableError(r.reconcileApproverPolicy(t.Context(), instance)))
	})
}
