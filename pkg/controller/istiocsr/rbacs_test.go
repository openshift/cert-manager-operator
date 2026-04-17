package istiocsr

import (
	"context"
	"maps"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestCreateOrApplyRBACResource(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
		postAssert                 func(t *testing.T, m *fakes.FakeCtrlClient)
	}{
		{
			name: "clusterrole reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.ClusterRole:
						return false, errTestClient
					}
					return true, nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRole = "cert-manager-istio-csr-sdghj"
			},
			wantErr: `failed to check /cert-manager-istio-csr-sdghj clusterrole resource already exists: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						return false, errTestClient
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
						return false, errTestClient
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
						return false, errTestClient
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
							return false, errTestClient
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
							return false, errTestClient
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
						return errTestClient
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
							*testClusterRoleBinding(),
							*testClusterRoleBindingExtra(),
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
							*testClusterRoleBinding(),
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
					case *rbacv1.ClusterRole:
						cr := testClusterRole()
						cr.SetName("cert-manager-istio-csr")
						cr.DeepCopyInto(o)
						return true, nil
					case *rbacv1.ClusterRoleBinding:
						clusterRoleBinding := testClusterRoleBinding()
						clusterRoleBinding.Labels = nil
						clusterRoleBinding.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						return errTestClient
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRole = "cert-manager-istio-csr"
				i.Status.ClusterRoleBinding = "cert-manager-istio-csr-dfkhk"
			},
			wantErr: `failed to update /cert-manager-istio-csr-dfkhk clusterrolebinding resource: test client error`,
			postAssert: func(t *testing.T, m *fakes.FakeCtrlClient) {
				assertClusterRoleBindingUpdateUsesLiveName(t, m, "cert-manager-istio-csr-dfkhk")
			},
		},
		{
			name: "clusterrolebindings reconciliation updating to desired state successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						cr := testClusterRole()
						cr.SetName("cert-manager-istio-csr")
						cr.DeepCopyInto(o)
						return true, nil
					case *rbacv1.ClusterRoleBinding:
						clusterRoleBinding := testClusterRoleBinding()
						clusterRoleBinding.Labels = nil
						clusterRoleBinding.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRole = "cert-manager-istio-csr"
				i.Status.ClusterRoleBinding = "cert-manager-istio-csr-dfkhk"
			},
			wantClusterRoleBindingName: "cert-manager-istio-csr-dfkhk",
			postAssert: func(t *testing.T, m *fakes.FakeCtrlClient) {
				assertClusterRoleBindingUpdateUsesLiveName(t, m, "cert-manager-istio-csr-dfkhk")
			},
		},
		{
			name: "clusterrolebindings reconciliation roleRef change delete fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						cr := testClusterRole()
						cr.SetName("cert-manager-istio-csr")
						cr.DeepCopyInto(o)
						return true, nil
					case *rbacv1.ClusterRoleBinding:
						clusterRoleBindingWithWrongRoleRef("stale-cluster-role").DeepCopyInto(o)
					}
					return true, nil
				})
				m.DeleteCalls(func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						return errTestClient
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRole = "cert-manager-istio-csr"
				i.Status.ClusterRoleBinding = "cert-manager-istio-csr-dfkhk"
			},
			wantErr: `failed to delete /cert-manager-istio-csr-dfkhk clusterrolebinding to replace roleRef: test client error`,
			postAssert: func(t *testing.T, m *fakes.FakeCtrlClient) {
				assertClusterRoleBindingRoleRefReplaceNoUpdate(t, m)
			},
		},
		{
			name: "clusterrolebindings reconciliation roleRef change delete succeeds and recreate succeeds",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						cr := testClusterRole()
						cr.SetName("cert-manager-istio-csr")
						cr.DeepCopyInto(o)
						return true, nil
					case *rbacv1.ClusterRoleBinding:
						clusterRoleBindingWithWrongRoleRef("stale-cluster-role").DeepCopyInto(o)
					}
					return true, nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRole = "cert-manager-istio-csr"
				i.Status.ClusterRoleBinding = "cert-manager-istio-csr-dfkhk"
			},
			wantClusterRoleBindingName: "cert-manager-istio-csr-dfkhk",
			postAssert: func(t *testing.T, m *fakes.FakeCtrlClient) {
				assertClusterRoleBindingRoleRefReplaceUsesDeleteCreate(t, m, "cert-manager-istio-csr-dfkhk", "cert-manager-istio-csr")
			},
		},
		{
			name: "clusterrolebindings reconciliation roleRef change delete succeeds and recreate fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						cr := testClusterRole()
						cr.SetName("cert-manager-istio-csr")
						cr.DeepCopyInto(o)
						return true, nil
					case *rbacv1.ClusterRoleBinding:
						clusterRoleBindingWithWrongRoleRef("stale-cluster-role").DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						return errTestClient
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRole = "cert-manager-istio-csr"
				i.Status.ClusterRoleBinding = "cert-manager-istio-csr-dfkhk"
			},
			wantErr: `failed to create /cert-manager-istio-csr-dfkhk clusterrolebinding resource: test client error`,
			postAssert: func(t *testing.T, m *fakes.FakeCtrlClient) {
				assertClusterRoleBindingRoleRefReplaceUsesDeleteCreate(t, m, "cert-manager-istio-csr-dfkhk", "cert-manager-istio-csr")
			},
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
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create  clusterrolebinding resource: test client error`,
		},
		{
			name: "clusterrole reconciliation updating name in status fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBindingList:
						clusterRoleBindingsList := &rbacv1.ClusterRoleBindingList{}
						clusterRoleBindingsList.Items = []rbacv1.ClusterRoleBinding{
							*testClusterRoleBinding(),
						}
						clusterRoleBindingsList.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						clusterRole := testClusterRole()
						clusterRole.Labels = nil
						clusterRole.DeepCopyInto(o)
					}
					return true, nil
				})
				m.StatusUpdateCalls(func(ctx context.Context, obj client.Object, option ...client.SubResourceUpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return errTestClient
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRole = "cert-manager-istio-csr-sdghj"
			},
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with cert-manager-istio-csr-sdghj clusterrole resource name: failed to update status for "istiocsr-test-ns/istiocsr-test-resource": failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation updating name in status fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, obj client.ObjectList, opts ...client.ListOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBindingList:
						clusterRoleBindingsList := &rbacv1.ClusterRoleBindingList{}
						clusterRoleBindingsList.Items = []rbacv1.ClusterRoleBinding{
							*testClusterRoleBinding(),
						}
						clusterRoleBindingsList.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						clusterRole := testClusterRole()
						clusterRole.Labels = nil
						clusterRole.DeepCopyInto(o)
					}
					return true, nil
				})
				m.StatusUpdateCalls(func(ctx context.Context, obj client.Object, option ...client.SubResourceUpdateOption) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						if o.Status.ClusterRoleBinding != "" {
							return errTestClient
						}
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRole = "cert-manager-istio-csr-sdghj"
			},
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with /cert-manager-istio-csr-dfkhk clusterrolebinding resource name: failed to update status for "istiocsr-test-ns/istiocsr-test-resource": failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
		},
		{
			name: "clusterrole reconciliation updating to desired state fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						clusterRole := testClusterRole()
						clusterRole.Labels = nil
						clusterRole.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRole:
						return errTestClient
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRole = "cert-manager-istio-csr-sdghj"
			},
			wantErr: `failed to update /cert-manager-istio-csr-sdghj clusterrole resource: test client error`,
			postAssert: func(t *testing.T, m *fakes.FakeCtrlClient) {
				assertClusterRoleUpdateUsesLiveName(t, m, "cert-manager-istio-csr-sdghj")
			},
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
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create  clusterrole resource: test client error`,
		},
		{
			name: "role reconciliation updating to desired state fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.Role:
						role := testRole()
						role.Labels = nil
						role.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.Role:
						return errTestClient
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
						return errTestClient
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
							role := testRoleLeases()
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
							return errTestClient
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
							return errTestClient
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
						role := testRoleBinding()
						role.Labels = nil
						role.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						return errTestClient
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
						return errTestClient
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
							role := testRoleBindingLeases()
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
							return errTestClient
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
							return errTestClient
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
			r.CtrlClient = mock
			istiocsr := testIstioCSR()
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
			if tt.postAssert != nil {
				wantOK := tt.wantErr == "" && err == nil
				wantFail := tt.wantErr != "" && err != nil && err.Error() == tt.wantErr
				if wantOK || wantFail {
					tt.postAssert(t, mock)
				}
			}
		})
	}
}

// assertClusterRoleBindingUpdateUsesLiveName fails if UpdateWithRetry was not invoked for a
// ClusterRoleBinding with the live metadata name and an empty GenerateName (regression guard for
// updates that previously sent only generateName).
func assertClusterRoleBindingUpdateUsesLiveName(t *testing.T, m *fakes.FakeCtrlClient, wantName string) {
	t.Helper()
	var crbUpdates int
	for i := 0; i < m.UpdateWithRetryCallCount(); i++ {
		_, obj, _ := m.UpdateWithRetryArgsForCall(i)
		if crb, ok := obj.(*rbacv1.ClusterRoleBinding); ok {
			crbUpdates++
			if crb.GetName() != wantName {
				t.Errorf("ClusterRoleBinding UpdateWithRetry: want metadata.name %q, got %q", wantName, crb.GetName())
			}
			if crb.GetGenerateName() != "" {
				t.Errorf("ClusterRoleBinding UpdateWithRetry: want empty generateName, got %q", crb.GetGenerateName())
			}
		}
	}
	if crbUpdates == 0 {
		t.Fatalf("expected at least one UpdateWithRetry for ClusterRoleBinding with live name set, got 0")
	}
}

// assertClusterRoleUpdateUsesLiveName is the ClusterRole analogue of assertClusterRoleBindingUpdateUsesLiveName.
func assertClusterRoleUpdateUsesLiveName(t *testing.T, m *fakes.FakeCtrlClient, wantName string) {
	t.Helper()
	var crUpdates int
	for i := 0; i < m.UpdateWithRetryCallCount(); i++ {
		_, obj, _ := m.UpdateWithRetryArgsForCall(i)
		if cr, ok := obj.(*rbacv1.ClusterRole); ok {
			crUpdates++
			if cr.GetName() != wantName {
				t.Errorf("ClusterRole UpdateWithRetry: want metadata.name %q, got %q", wantName, cr.GetName())
			}
			if cr.GetGenerateName() != "" {
				t.Errorf("ClusterRole UpdateWithRetry: want empty generateName, got %q", cr.GetGenerateName())
			}
		}
	}
	if crUpdates == 0 {
		t.Fatalf("expected at least one UpdateWithRetry for ClusterRole with live name set, got 0")
	}
}

func assertNoClusterRoleBindingUpdateWithRetry(t *testing.T, m *fakes.FakeCtrlClient) {
	t.Helper()
	for i := 0; i < m.UpdateWithRetryCallCount(); i++ {
		_, obj, _ := m.UpdateWithRetryArgsForCall(i)
		if _, ok := obj.(*rbacv1.ClusterRoleBinding); ok {
			t.Fatalf("expected no UpdateWithRetry on ClusterRoleBinding for roleRef replace path, got call index %d", i)
		}
	}
}

func clusterRoleBindingDeleteCount(m *fakes.FakeCtrlClient) int {
	n := 0
	for i := 0; i < m.DeleteCallCount(); i++ {
		_, obj, _ := m.DeleteArgsForCall(i)
		if _, ok := obj.(*rbacv1.ClusterRoleBinding); ok {
			n++
		}
	}
	return n
}

// assertClusterRoleBindingRoleRefReplaceNoUpdate checks roleRef reconciliation attempted delete and
// did not fall through to UpdateWithRetry on the binding (e.g. delete failed first).
func assertClusterRoleBindingRoleRefReplaceNoUpdate(t *testing.T, m *fakes.FakeCtrlClient) {
	t.Helper()
	assertNoClusterRoleBindingUpdateWithRetry(t, m)
	if clusterRoleBindingDeleteCount(m) == 0 {
		t.Fatalf("expected Delete on ClusterRoleBinding for roleRef replace")
	}
}

// assertClusterRoleBindingRoleRefReplaceUsesDeleteCreate checks a RoleRef mismatch is corrected via
// Delete + Create, not UpdateWithRetry, and the create uses the stable binding name and desired RoleRef.
func assertClusterRoleBindingRoleRefReplaceUsesDeleteCreate(t *testing.T, m *fakes.FakeCtrlClient, wantBindingName, wantRoleRefName string) {
	t.Helper()
	assertNoClusterRoleBindingUpdateWithRetry(t, m)
	if clusterRoleBindingDeleteCount(m) == 0 {
		t.Fatalf("expected Delete on ClusterRoleBinding for roleRef replace")
	}
	var sawCreate bool
	for i := 0; i < m.CreateCallCount(); i++ {
		_, obj, _ := m.CreateArgsForCall(i)
		crb, ok := obj.(*rbacv1.ClusterRoleBinding)
		if !ok {
			continue
		}
		sawCreate = true
		if crb.GetName() != wantBindingName {
			t.Errorf("Create ClusterRoleBinding: want metadata.name %q, got %q", wantBindingName, crb.GetName())
		}
		if crb.GetGenerateName() != "" {
			t.Errorf("Create ClusterRoleBinding: want empty generateName for stable recreate, got %q", crb.GetGenerateName())
		}
		if crb.RoleRef.Name != wantRoleRefName {
			t.Errorf("Create ClusterRoleBinding RoleRef.Name: want %q, got %q", wantRoleRefName, crb.RoleRef.Name)
		}
	}
	if !sawCreate {
		t.Fatalf("expected Create on ClusterRoleBinding after roleRef replace delete")
	}
}

// clusterRoleBindingWithWrongRoleRef returns a binding aligned with the reconciler's desired object
// (labels, subjects) but pointing at a different ClusterRole, so roleRef reconciliation deletes and recreates.
func clusterRoleBindingWithWrongRoleRef(wrongRoleName string) *rbacv1.ClusterRoleBinding {
	b := testClusterRoleBinding()
	b.RoleRef.Name = wrongRoleName
	labels := make(map[string]string, len(controllerDefaultResourceLabels)+1)
	maps.Copy(labels, controllerDefaultResourceLabels)
	labels[istiocsrNamespaceMappingLabelName] = testIstioCSRNamespace
	b.SetLabels(labels)
	b.Subjects[0].Namespace = testIstioCSRNamespace
	return b
}
