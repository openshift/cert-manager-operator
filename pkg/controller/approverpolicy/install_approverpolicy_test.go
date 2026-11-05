package approverpolicy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func desiredResources(t *testing.T, instance *v1alpha1.ApproverPolicyManager) []client.Object {
	t.Helper()
	deployment, err := getDeploymentObject(instance, "")
	require.NoError(t, err)
	resources := []client.Object{getServiceAccountObject(instance), getSecretObject(instance)}
	resources = append(resources, getRBACObjects(instance)...)
	resources = append(resources, getServiceObjects(instance)...)
	return append(resources, deployment, getWebhookObject(instance))
}

func objectID(obj client.Object) string {
	return fmt.Sprintf("%T/%s", obj, client.ObjectKeyFromObject(obj))
}

func copyObject(dst, src client.Object) {
	reflect.ValueOf(dst).Elem().Set(reflect.ValueOf(src.DeepCopyObject()).Elem())
}

func TestApplyResources(t *testing.T) {
	t.Setenv(imageEnvVar, "example.test/approver:test")
	instance := testInstance()
	instance.Spec.ControllerConfig.Labels = map[string]string{"team": "security", "app": "override"}
	instance.Spec.ControllerConfig.Annotations = map[string]string{"example.test/note": "value"}
	objects := desiredResources(t, instance)
	require.Len(t, objects, 10)
	for _, obj := range objects {
		t.Run(objectID(obj), func(t *testing.T) {
			c := &fakes.FakeCtrlClient{}
			c.GetReturns(apierrors.NewNotFound(v1alpha1.Resource("managed"), obj.GetName()))
			recorder := record.NewFakeRecorder(10)
			r := &Reconciler{CtrlClient: c, log: logr.Discard(), eventRecorder: recorder}

			var applied client.Object
			t.Run("creates the resource via a forced SSA Patch", func(t *testing.T) {
				require.NoError(t, r.applyResource(t.Context(), instance, obj))
				require.Equal(t, 1, c.PatchCallCount())
				var patch client.Patch
				var opts []client.PatchOption
				_, applied, patch, opts = c.PatchArgsForCall(0)
				assert.Equal(t, types.ApplyPatchType, patch.Type())
				options := &client.PatchOptions{}
				options.ApplyOptions(opts)
				assert.Equal(t, fieldOwner, options.FieldManager)
				require.NotNil(t, options.Force)
				assert.True(t, *options.Force)
				assert.True(t, metav1.IsControlledBy(applied, instance))
				assert.Equal(t, operandName, applied.GetLabels()["app"])
				assert.Equal(t, "security", applied.GetLabels()["team"])
				assert.Equal(t, "value", applied.GetAnnotations()["example.test/note"])
				assert.Zero(t, c.CreateCallCount())
				assert.Zero(t, c.UpdateCallCount())
				assert.Len(t, recorder.Events, 1)
			})

			t.Run("re-applying an already-desired resource is a no-op", func(t *testing.T) {
				// A persisted object with unrelated metadata is already desired.
				existing := applied.DeepCopyObject().(client.Object)
				existing.GetAnnotations()["external.example/note"] = "keep"
				c.GetCalls(func(_ context.Context, _ client.ObjectKey, target client.Object) error {
					copyObject(target, existing)
					return nil
				})

				require.NoError(t, r.applyResource(t.Context(), instance, obj))

				assert.Equal(t, 1, c.PatchCallCount(), "no write on an unchanged resource")
				assert.Len(t, recorder.Events, 1, "no new event either")
			})
		})
	}
}

func TestApplyResourceFailures(t *testing.T) {
	instance := testInstance()
	for _, tc := range []struct {
		name        string
		readErr     error
		patchErr    error
		foreign     bool
		terminating bool
		permanent   bool
	}{
		{name: "read forbidden", readErr: apierrors.NewForbidden(v1alpha1.Resource("managed"), "x", errors.New("denied")), permanent: true},
		{name: "read timeout", readErr: apierrors.NewTimeoutError("timeout", 1)},
		{name: "apply conflict", patchErr: apierrors.NewConflict(v1alpha1.Resource("managed"), "x", errors.New("conflict"))},
		{name: "apply forbidden", patchErr: apierrors.NewForbidden(v1alpha1.Resource("managed"), "x", errors.New("denied")), permanent: true},
		{name: "foreign ownership", foreign: true, permanent: true},
		{name: "terminating", terminating: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &fakes.FakeCtrlClient{}
			c.GetCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				if tc.readErr != nil {
					return tc.readErr
				}
				if tc.foreign || tc.terminating {
					copyObject(obj, getSecretObject(instance))
					if tc.foreign {
						obj.SetOwnerReferences(nil)
					}
					if tc.terminating {
						now := metav1.Now()
						obj.SetDeletionTimestamp(&now)
					}
					return nil
				}
				return apierrors.NewNotFound(v1alpha1.Resource("managed"), "x")
			})
			c.PatchReturns(tc.patchErr)
			r := &Reconciler{CtrlClient: c, log: logr.Discard()}
			err := r.applyResource(t.Context(), instance, getSecretObject(instance))
			require.Error(t, err)
			assert.Equal(t, tc.permanent, common.IsIrrecoverableError(err))
			if tc.foreign || tc.terminating || tc.readErr != nil {
				assert.Zero(t, c.PatchCallCount())
			}
		})
	}
}

func TestApplyResourceUsesCacheBeforeAPI(t *testing.T) {
	instance := testInstance()
	owned := getServiceAccountObject(instance)
	owned.UID = "account-uid"
	owned.ResourceVersion = "7"
	unlabeled := owned.DeepCopy()
	unlabeled.Labels = nil
	foreign := unlabeled.DeepCopy()
	foreign.OwnerReferences = nil
	readErr := apierrors.NewTimeoutError("read timed out", 1)

	tests := []struct {
		name          string
		cached        *corev1.ServiceAccount
		cacheErr      error
		live          *corev1.ServiceAccount
		apiErr        error
		wantAPIReads  int
		wantPatches   int
		wantError     bool
		wantPermanent bool
	}{
		{
			name:   "cached resource needs no API read",
			cached: owned,
		},
		{
			name:      "cache read error is returned",
			cacheErr:  readErr,
			wantError: true,
		},
		{
			name:         "cache miss repairs an owned resource with missing labels",
			live:         unlabeled,
			wantAPIReads: 1,
			wantPatches:  1,
		},
		{
			name:          "cache miss cannot adopt a foreign resource",
			live:          foreign,
			wantAPIReads:  1,
			wantError:     true,
			wantPermanent: true,
		},
		{
			name:         "resource absent from cache and API is created",
			wantAPIReads: 1,
			wantPatches:  1,
		},
		{
			name:         "API failure after cache miss prevents apply",
			apiErr:       readErr,
			wantAPIReads: 1,
			wantError:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &fakes.FakeCtrlClient{}
			c.GetCalls(func(_ context.Context, key client.ObjectKey, obj client.Object) error {
				if tt.cacheErr != nil {
					return tt.cacheErr
				}
				if tt.cached == nil {
					return apierrors.NewNotFound(corev1.Resource("serviceaccounts"), key.Name)
				}
				copyObject(obj, tt.cached)
				return nil
			})
			api := &testAPIReader{getErr: tt.apiErr}
			if tt.live != nil {
				api.objects = []client.Object{tt.live}
			}
			r := &Reconciler{CtrlClient: c, apiReader: api, log: logr.Discard()}

			err := r.applyResource(t.Context(), instance, getServiceAccountObject(instance))
			if tt.wantError {
				require.Error(t, err)
				assert.Equal(t, tt.wantPermanent, common.IsIrrecoverableError(err))
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, 1, c.GetCallCount())
			assert.Equal(t, tt.wantAPIReads, api.getCalls)
			require.Equal(t, tt.wantPatches, c.PatchCallCount())
			if tt.wantPatches > 0 && tt.live != nil {
				_, applied, _, _ := c.PatchArgsForCall(0)
				assert.Equal(t, tt.live.UID, applied.GetUID())
				assert.Equal(t, tt.live.ResourceVersion, applied.GetResourceVersion())
			}
		})
	}
}

func TestInstallWaitsForCachedResources(t *testing.T) {
	for _, scenario := range []string{"ServiceAccount not cached yet", "Deployment not cached yet", "Deployment cache contains previous rollout"} {
		t.Run(scenario, func(t *testing.T) {
			instance := testInstance()
			r, c := testReconciler(t, instance)
			api := &testAPIReader{objects: desiredResources(t, instance)}
			r.apiReader = api
			cachedGet := c.GetStub
			c.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
				if scenario == "ServiceAccount not cached yet" {
					if _, account := obj.(*corev1.ServiceAccount); account {
						return apierrors.NewNotFound(corev1.Resource("serviceaccounts"), key.Name)
					}
				}
				if scenario == "Deployment not cached yet" {
					if _, deployment := obj.(*appsv1.Deployment); deployment {
						return apierrors.NewNotFound(appsv1.Resource("deployments"), key.Name)
					}
				}
				if err := cachedGet(ctx, key, obj); err != nil {
					return err
				}
				if scenario == "Deployment cache contains previous rollout" {
					if deployment, ok := obj.(*appsv1.Deployment); ok {
						deployment.Spec.Template.Spec.Containers[0].Image = "example.test/approver-policy:previous"
						deployment.Generation = 1
						deployment.Status = availableDeploymentStatus(1)
					}
				}
				return nil
			})

			err := r.reconcileApproverPolicy(t.Context(), instance)
			require.Error(t, err)
			assert.True(t, common.IsRetryRequiredError(err), "wait for the cache instead of reporting ready or degraded")
			if scenario == "Deployment cache contains previous rollout" {
				assert.Zero(t, api.getCalls, "routine reads should stay in the cache")
				require.Equal(t, 1, c.PatchCallCount())
				assert.Equal(t, "example.test/approver-policy:test", instance.Status.ApproverPolicyImage,
					"image status reflects the desired Deployment while the cache contains the previous rollout")
			} else {
				assert.Equal(t, 1, api.getCalls, "only the apply ownership check confirms the cache miss")
				assert.Zero(t, c.PatchCallCount())
			}
		})
	}
}

func TestInstallRolloutAndServiceAccountRecreation(t *testing.T) {
	instance := testInstance()
	r, c := testReconciler(t, instance)
	primaryGet := c.GetStub
	objects := map[string]client.Object{}
	c.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if _, primary := obj.(*v1alpha1.ApproverPolicyManager); primary {
			return primaryGet(ctx, key, obj)
		}
		obj.SetName(key.Name)
		obj.SetNamespace(key.Namespace)
		existing, ok := objects[objectID(obj)]
		if !ok {
			return apierrors.NewNotFound(v1alpha1.Resource("managed"), key.Name)
		}
		copyObject(obj, existing)
		return nil
	})
	c.PatchCalls(func(_ context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
		assert.Contains(t, instance.Finalizers, finalizer, "finalizer must persist before the first apply")
		obj.SetUID("resource-uid")
		obj.SetGeneration(obj.GetGeneration() + 1)
		objects[objectID(obj)] = obj.DeepCopyObject().(client.Object)
		return nil
	})
	result, err := r.Reconcile(t.Context(), singletonRequest())
	require.NoError(t, err)
	assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
	assert.Len(t, objects, 9)
	appliedKinds := make([]string, 0, c.PatchCallCount())
	for i := range c.PatchCallCount() {
		_, applied, _, _ := c.PatchArgsForCall(i)
		appliedKinds = append(appliedKinds, applied.GetObjectKind().GroupVersionKind().Kind)
	}
	assert.Equal(t, []string{
		"ServiceAccount", "Secret", "ClusterRole", "ClusterRoleBinding", "Role", "RoleBinding",
		"Service", "Service", "Deployment",
	}, appliedKinds, "install prerequisites and the workload before registering the webhook")
	webhookID := objectID(getWebhookObject(instance))
	assert.NotContains(t, objects, webhookID, "do not register a webhook with an unready backend")
	assertCondition(t, instance, v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonInProgress)
	assert.Equal(t, "example.test/approver-policy:test", instance.Status.ApproverPolicyImage)
	deployment := objects[objectID(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: operandName, Namespace: operandNamespace}})].(*appsv1.Deployment)
	assert.Equal(t, "resource-uid", deployment.Spec.Template.Annotations[serviceAccountUIDAnnotation],
		"the Deployment uses the UID read back after applying the ServiceAccount")
	patches := c.PatchCallCount()
	result, err = r.Reconcile(t.Context(), singletonRequest())
	require.NoError(t, err)
	assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
	assert.Equal(t, patches, c.PatchCallCount(), "waiting for readiness does not reapply resources")
	assert.NotContains(t, objects, webhookID)

	// Register the webhook once the Deployment is observed ready.
	deployment.Status = availableDeploymentStatus(deployment.Generation)
	result, err = r.Reconcile(t.Context(), singletonRequest())
	require.NoError(t, err)
	assert.Zero(t, result)
	assertCondition(t, instance, v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady)
	require.Equal(t, patches+1, c.PatchCallCount(), "only the webhook is applied after readiness")
	_, applied, _, _ := c.PatchArgsForCall(patches)
	assert.IsType(t, &admissionregistrationv1.ValidatingWebhookConfiguration{}, applied)
	require.Contains(t, objects, webhookID)
	webhook := objects[webhookID]
	patches = c.PatchCallCount()
	_, err = r.Reconcile(t.Context(), singletonRequest())
	require.NoError(t, err)
	assertCondition(t, instance, v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady)
	assert.Equal(t, patches, c.PatchCallCount())
	account := objects[objectID(getServiceAccountObject(instance))]
	account.SetUID("replacement-account")
	result, err = r.Reconcile(t.Context(), singletonRequest())
	require.NoError(t, err)
	assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
	assertCondition(t, instance, v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonInProgress)
	assert.Equal(t, patches+1, c.PatchCallCount(), "only the Deployment rolls for the new account UID")
	_, applied, _, _ = c.PatchArgsForCall(patches)
	assert.Equal(t, "replacement-account", applied.(*appsv1.Deployment).Spec.Template.Annotations[serviceAccountUIDAnnotation])
	assert.Same(t, webhook, objects[webhookID], "an existing webhook remains registered during a rollout")
	assert.Zero(t, c.DeleteCallCount())
}

func availableDeploymentStatus(generation int64) appsv1.DeploymentStatus {
	return appsv1.DeploymentStatus{ObservedGeneration: generation, Replicas: 1,
		UpdatedReplicas: 1, ReadyReplicas: 1, AvailableReplicas: 1,
		Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}}}
}

// TestReconcileApproverPolicyFailurePaths verifies that a build or apply failure
// stops installation immediately and surfaces the underlying error.
func TestReconcileApproverPolicyFailurePaths(t *testing.T) {
	applyErr := errors.New("apply denied")
	for _, tc := range []struct {
		name          string
		mutate        func(*v1alpha1.ApproverPolicyManager)
		failOnKind    reflect.Type
		wantPermanent bool
	}{
		{
			name:       "the ServiceAccount apply is rejected",
			failOnKind: reflect.TypeOf(&corev1.ServiceAccount{}),
		},
		{
			name:       "the TLS Secret apply is rejected",
			failOnKind: reflect.TypeOf(&corev1.Secret{}),
		},
		{
			name:       "an RBAC apply is rejected",
			failOnKind: reflect.TypeOf(&rbacv1.ClusterRole{}),
		},
		{
			name:       "a Service apply is rejected",
			failOnKind: reflect.TypeOf(&corev1.Service{}),
		},
		{
			name: "the Deployment cannot be built from an invalid configuration",
			mutate: func(o *v1alpha1.ApproverPolicyManager) {
				o.Spec.ApproverPolicyConfig.NodeSelector = map[string]string{"invalid!": "x"}
			},
			wantPermanent: true,
		},
		{
			name:       "the Deployment apply is rejected",
			failOnKind: reflect.TypeOf(&appsv1.Deployment{}),
		},
		{
			name:       "the ValidatingWebhookConfiguration apply is rejected",
			failOnKind: reflect.TypeOf(&admissionregistrationv1.ValidatingWebhookConfiguration{}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instance := testInstance()
			if tc.mutate != nil {
				tc.mutate(instance)
			}
			r, c := testReconciler(t, instance)
			// Apply reads report resources absent so each apply is attempted.
			// Subsequent reads provide the ServiceAccount UID and a ready
			// Deployment so the webhook apply can also be reached.
			c.GetCalls(func(_ context.Context, key client.ObjectKey, obj client.Object) error {
				if sa, ok := obj.(*corev1.ServiceAccount); ok && len(sa.GetLabels()) == 0 {
					sa.UID = "sa-uid"
					return nil
				}
				if deployment, ok := obj.(*appsv1.Deployment); ok && len(deployment.Labels) == 0 {
					desired, err := getDeploymentObject(instance, "sa-uid")
					require.NoError(t, err)
					copyObject(deployment, desired)
					deployment.Status = availableDeploymentStatus(deployment.Generation)
					return nil
				}
				return apierrors.NewNotFound(v1alpha1.Resource("managed"), key.Name)
			})
			c.PatchCalls(func(_ context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
				if tc.failOnKind != nil && reflect.TypeOf(obj) == tc.failOnKind {
					return applyErr
				}
				return nil
			})

			err := r.reconcileApproverPolicy(t.Context(), instance)
			require.Error(t, err)
			assert.Equal(t, tc.wantPermanent, common.IsIrrecoverableError(err))
			if tc.failOnKind != nil {
				assert.ErrorIs(t, err, applyErr, "the original apply error must still be reachable")
				require.Positive(t, c.PatchCallCount())
				_, lastApplied, _, _ := c.PatchArgsForCall(c.PatchCallCount() - 1)
				assert.Equal(t, tc.failOnKind, reflect.TypeOf(lastApplied), "do not apply later resources after a failure")
			}
		})
	}
}
