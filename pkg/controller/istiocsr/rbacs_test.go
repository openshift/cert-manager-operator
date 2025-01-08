package istiocsr

import (
	"context"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr/fakes"
)

func TestCreateOrApplyRBACResource(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "clusterrole reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.ClusterRole:
						return false, testError
					}
					return true, nil
				})
			},
			wantErr: `failed to check /cert-manager-istio-csr clusterrole resource already exists: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						return false, testError
					}
					return true, nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRoleBinding = "cert-manager-istio-csr-dfkhk"
			},
			wantErr: `failed to check /cert-manager-istio-csr-dfkhk clusterrolebinding resource already exists: test client error`,
		},
		{
			name: "role reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.Role:
						return false, testError
					}
					return true, nil
				})
			},
			wantErr: `failed to check istio-test-ns/cert-manager-istio-csr role resource already exists: test client error`,
		},
		{
			name: "rolebindings reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						return false, testError
					}
					return true, nil
				})
			},
			wantErr: `failed to check istio-test-ns/cert-manager-istio-csr rolebinding resource already exists: test client error`,
		},
		{
			name: "role-leases reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.Role:
						if strings.HasSuffix(ns.Name, "-leases") {
							return false, testError
						}
					}
					return true, nil
				})
			},
			wantErr: `failed to check istio-test-ns/cert-manager-istio-csr-leases role resource already exists: test client error`,
		},
		{
			name: "rolebindings-leases reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						if strings.HasSuffix(ns.Name, "-leases") {
							return false, testError
						}
					}
					return true, nil
				})
			},
			wantErr: `failed to check istio-test-ns/cert-manager-istio-csr-leases rolebinding resource already exists: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation fails while listing existing resources",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBindingList:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to list clusterrolebinding resources, impacted namespace istiocsr-test-ns: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation fails while listing multiple existing resources",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBindingList:
						clusterRoleBindingsList := &rbacv1.ClusterRoleBindingList{}
						clusterRoleBindingsList.Items = []rbacv1.ClusterRoleBinding{
							*testClusterRoleBinding(t),
							*testClusterRoleBindingExtra(t),
						}
						clusterRoleBindingsList.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `matched clusterrolebinding resources: [{TypeMeta:{Kind:ClusterRoleBinding APIVersion:rbac.authorization.k8s.io/v1} ObjectMeta:{Name:cert-manager-istio-csr-dfkhk GenerateName:cert-manager-istio-csr- Namespace: SelfLink: UID: ResourceVersion: Generation:0 CreationTimestamp:0001-01-01 00:00:00 +0000 UTC DeletionTimestamp:<nil> DeletionGracePeriodSeconds:<nil> Labels:map[app:cert-manager-istio-csr app.kubernetes.io/instance:cert-manager-istio-csr app.kubernetes.io/managed-by:cert-manager-operator app.kubernetes.io/name:cert-manager-istio-csr app.kubernetes.io/part-of:cert-manager-operator app.kubernetes.io/version:] Annotations:map[] OwnerReferences:[] Finalizers:[] ManagedFields:[]} Subjects:[{Kind:ServiceAccount APIGroup: Name:cert-manager-istio-csr Namespace:cert-manager}] RoleRef:{APIGroup:rbac.authorization.k8s.io Kind:ClusterRole Name:cert-manager-istio-csr}} {TypeMeta:{Kind:ClusterRoleBinding APIVersion:rbac.authorization.k8s.io/v1} ObjectMeta:{Name:cert-manager-istio-csr-dfmfj GenerateName:cert-manager-istio-csr- Namespace: SelfLink: UID: ResourceVersion: Generation:0 CreationTimestamp:0001-01-01 00:00:00 +0000 UTC DeletionTimestamp:<nil> DeletionGracePeriodSeconds:<nil> Labels:map[app:cert-manager-istio-csr app.kubernetes.io/instance:cert-manager-istio-csr app.kubernetes.io/managed-by:cert-manager-operator app.kubernetes.io/name:cert-manager-istio-csr app.kubernetes.io/part-of:cert-manager-operator app.kubernetes.io/version:] Annotations:map[] OwnerReferences:[] Finalizers:[] ManagedFields:[]} Subjects:[{Kind:ServiceAccount APIGroup: Name:cert-manager-istio-csr Namespace:cert-manager}] RoleRef:{APIGroup:rbac.authorization.k8s.io Kind:ClusterRole Name:cert-manager-istio-csr}}]: more than 1 clusterrolebinding resources exist with matching labels`,
		},
		{
			name: "clusterrolebindings reconciliation successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBindingList:
						clusterRoleBindingsList := &rbacv1.ClusterRoleBindingList{}
						clusterRoleBindingsList.Items = []rbacv1.ClusterRoleBinding{
							*testClusterRoleBinding(t),
						}
						clusterRoleBindingsList.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantClusterRoleBindingName: "cert-manager-istio-csr-dfkhk",
		},
		{
			name: "clusterrolebindings reconciliation updating to desired state fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRoleBinding:
						clusterRoleBinding := testClusterRoleBinding(t)
						clusterRoleBinding.Labels = nil
						clusterRoleBinding.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						return testError
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRoleBinding = "cert-manager-istio-csr-dfkhk"
			},
			wantErr: `failed to update /cert-manager-istio-csr-dfkhk clusterrolebinding resource: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation updating to desired state successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRoleBinding:
						clusterRoleBinding := testClusterRoleBinding(t)
						clusterRoleBinding.Labels = nil
						clusterRoleBinding.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRoleBinding = "cert-manager-istio-csr-dfkhk"
			},
			wantClusterRoleBindingName: "cert-manager-istio-csr-dfkhk",
		},
		{
			name: "clusterrolebindings reconciliation creation fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.ClusterRoleBinding:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to create  clusterrolebinding resource: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation updating name is status fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBindingList:
						clusterRoleBindingsList := &rbacv1.ClusterRoleBindingList{}
						clusterRoleBindingsList.Items = []rbacv1.ClusterRoleBinding{
							*testClusterRoleBinding(t),
						}
						clusterRoleBindingsList.DeepCopyInto(o)
					}
					return nil
				})
				m.StatusUpdateCalls(func(ctx context.Context, obj client.Object, option ...client.SubResourceUpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with /cert-manager-istio-csr-dfkhk clusterrolebinding resource name: failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
		},
		{
			name: "clusterrole reconciliation updating to desired state fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						clusterRole := testClusterRole(t)
						clusterRole.Labels = nil
						clusterRole.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRole:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to update /cert-manager-istio-csr clusterrole resource: test client error`,
		},
		{
			name: "clusterrole reconciliation creation fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.ClusterRole:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRole:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to create /cert-manager-istio-csr clusterrole resource: test client error`,
		},
		{
			name: "role reconciliation updating to desired state fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.Role:
						role := testRole(t)
						role.Labels = nil
						role.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.Role:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to update istio-test-ns/cert-manager-istio-csr role resource: test client error`,
		},
		{
			name: "role reconciliation creation fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.Role:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.Role:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to create istio-test-ns/cert-manager-istio-csr role resource: test client error`,
		},
		{
			name: "role-leases reconciliation updating to desired state fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.Role:
						if strings.HasSuffix(ns.Name, "-leases") {
							role := testRoleLeases(t)
							role.Labels = nil
							role.DeepCopyInto(o)
						}
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.Role:
						if strings.HasSuffix(obj.GetName(), "-leases") {
							return testError
						}
					}
					return nil
				})
			},
			wantErr: `failed to update istio-test-ns/cert-manager-istio-csr-leases role resource: test client error`,
		},
		{
			name: "role-leases reconciliation creation fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.Role:
						if strings.HasSuffix(ns.Name, "-leases") {
							return false, nil
						}
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.Role:
						if strings.HasSuffix(obj.GetName(), "-leases") {
							return testError
						}
					}
					return nil
				})
			},
			wantErr: `failed to create istio-test-ns/cert-manager-istio-csr-leases role resource: test client error`,
		},
		{
			name: "rolebindings reconciliation updating to desired state fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.RoleBinding:
						role := testRoleBinding(t)
						role.Labels = nil
						role.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to update istio-test-ns/cert-manager-istio-csr rolebinding resource: test client error`,
		},
		{
			name: "rolebindings reconciliation creation fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.RoleBinding:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to create istio-test-ns/cert-manager-istio-csr rolebinding resource: test client error`,
		},
		{
			name: "rolebinding-leases reconciliation updating to desired state fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.RoleBinding:
						if strings.HasSuffix(ns.Name, "-leases") {
							role := testRoleBindingLeases(t)
							role.Labels = nil
							role.DeepCopyInto(o)
						}
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						if strings.HasSuffix(obj.GetName(), "-leases") {
							return testError
						}
					}
					return nil
				})
			},
			wantErr: `failed to update istio-test-ns/cert-manager-istio-csr-leases rolebinding resource: test client error`,
		},
		{
			name: "rolebinding-leases reconciliation creation fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.RoleBinding:
						if strings.HasSuffix(ns.Name, "-leases") {
							return false, nil
						}
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						if strings.HasSuffix(obj.GetName(), "-leases") {
							return testError
						}
					}
					return nil
				})
			},
			wantErr: `failed to create istio-test-ns/cert-manager-istio-csr-leases rolebinding resource: test client error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.ctrlClient = mock
			istiocsr := testIstioCSR(t)
			if tt.updateIstioCSR != nil {
				tt.updateIstioCSR(istiocsr)
			}
			err := r.createOrApplyRBACResource(istiocsr, controllerDefaultResourceLabels, true)
			if (tt.wantErr != "" || err != nil) && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("createOrApplyRBACResource() err: %v, wantErr: %v", err, tt.wantErr)
			}
			if tt.wantClusterRoleBindingName != "" {
				if istiocsr.Status.ClusterRoleBinding != tt.wantClusterRoleBindingName {
					t.Errorf("createOrApplyRBACResource() got: %v, want: %v", istiocsr.Status.ClusterRoleBinding, tt.wantClusterRoleBindingName)
				}
			}
		})
	}
}
