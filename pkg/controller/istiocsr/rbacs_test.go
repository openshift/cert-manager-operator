package istiocsr

import (
	"context"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr/fakes"
)

func TestCreateOrApplyRBACResource(t *testing.T) {
	t.Run("existence check errors", func(t *testing.T) {
		testCreateOrApplyRBACResource_ExistenceErrors(t)
	})
	t.Run("listing errors", func(t *testing.T) {
		testCreateOrApplyRBACResource_ListingErrors(t)
	})
	t.Run("cluster role binding cases", func(t *testing.T) {
		testCreateOrApplyRBACResource_ClusterRoleBinding(t)
	})
	t.Run("cluster role cases", func(t *testing.T) {
		testCreateOrApplyRBACResource_ClusterRole(t)
	})
	t.Run("role and role binding cases", func(t *testing.T) {
		testCreateOrApplyRBACResource_RoleAndBinding(t)
	})
}

func testCreateOrApplyRBACResource_ExistenceErrors(t *testing.T) {
	t.Run("cluster resource errors", func(t *testing.T) {
		testCreateOrApplyRBACResource_ExistenceErrors_Cluster(t)
	})
	t.Run("role resource errors", func(t *testing.T) {
		testCreateOrApplyRBACResource_ExistenceErrors_Role(t)
	})
}

func testCreateOrApplyRBACResource_ExistenceErrors_Cluster(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "clusterrole reconciliation fails while checking if exists",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
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
			wantErr: `failed to create or apply cluster roles: failed to find existing cluster role: failed to check /cert-manager-istio-csr-sdghj clusterrole resource already exists: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation fails while checking if exists",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
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
			wantErr: `failed to create or apply cluster role bindings: failed to find existing cluster role binding: failed to check /cert-manager-istio-csr-dfkhk clusterrolebinding resource already exists: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation fails while listing existing resources",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(_ context.Context, obj client.ObjectList, _ ...client.ListOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBindingList:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create or apply cluster role bindings: failed to find existing cluster role binding: failed to list clusterrolebinding resources, impacted namespace istiocsr-test-ns: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation fails while listing multiple existing resources",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(_ context.Context, obj client.ObjectList, _ ...client.ListOption) error {
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
			wantErr: `failed to create or apply cluster role bindings: failed to find existing cluster role binding: matched clusterrolebinding resources: [{TypeMeta:{Kind:ClusterRoleBinding APIVersion:rbac.authorization.k8s.io/v1} ObjectMeta:{Name:cert-manager-istio-csr-dfkhk GenerateName:cert-manager-istio-csr- Namespace: SelfLink: UID: ResourceVersion: Generation:0 CreationTimestamp:0001-01-01 00:00:00 +0000 UTC DeletionTimestamp:<nil> DeletionGracePeriodSeconds:<nil> Labels:map[app:cert-manager-istio-csr app.kubernetes.io/instance:cert-manager-istio-csr app.kubernetes.io/managed-by:cert-manager-operator app.kubernetes.io/name:cert-manager-istio-csr app.kubernetes.io/part-of:cert-manager-operator app.kubernetes.io/version:] Annotations:map[] OwnerReferences:[] Finalizers:[] ManagedFields:[]} Subjects:[{Kind:ServiceAccount APIGroup: Name:cert-manager-istio-csr Namespace:cert-manager}] RoleRef:{APIGroup:rbac.authorization.k8s.io Kind:ClusterRole Name:cert-manager-istio-csr}} {TypeMeta:{Kind:ClusterRoleBinding APIVersion:rbac.authorization.k8s.io/v1} ObjectMeta:{Name:cert-manager-istio-csr-dfmfj GenerateName:cert-manager-istio-csr- Namespace: SelfLink: UID: ResourceVersion: Generation:0 CreationTimestamp:0001-01-01 00:00:00 +0000 UTC DeletionTimestamp:<nil> DeletionGracePeriodSeconds:<nil> Labels:map[app:cert-manager-istio-csr app.kubernetes.io/instance:cert-manager-istio-csr app.kubernetes.io/managed-by:cert-manager-operator app.kubernetes.io/name:cert-manager-istio-csr app.kubernetes.io/part-of:cert-manager-operator app.kubernetes.io/version:] Annotations:map[] OwnerReferences:[] Finalizers:[] ManagedFields:[]} Subjects:[{Kind:ServiceAccount APIGroup: Name:cert-manager-istio-csr Namespace:cert-manager}] RoleRef:{APIGroup:rbac.authorization.k8s.io Kind:ClusterRole Name:cert-manager-istio-csr}}]: more than 1 clusterrolebinding resources exist with matching labels`,
		},
	}
	runRBACTests(t, tests)
}

func testCreateOrApplyRBACResource_ExistenceErrors_Role(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "role reconciliation fails while checking if exists",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.Role:
						return false, errTestClient
					}
					return true, nil
				})
			},
			wantErr: `failed to create or apply roles: failed to check istio-test-ns/cert-manager-istio-csr role resource already exists: test client error`,
		},
		{
			name: "rolebindings reconciliation fails while checking if exists",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						return false, errTestClient
					}
					return true, nil
				})
			},
			wantErr: `failed to create or apply role bindings: failed to check istio-test-ns/cert-manager-istio-csr rolebinding resource already exists: test client error`,
		},
		{
			name: "role-leases reconciliation fails while checking if exists",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.Role:
						if strings.HasSuffix(ns.Name, "-leases") {
							return false, errTestClient
						}
					}
					return true, nil
				})
			},
			wantErr: `failed to create or apply role for leases: failed to check istio-test-ns/cert-manager-istio-csr-leases role resource already exists: test client error`,
		},
		{
			name: "rolebindings-leases reconciliation fails while checking if exists",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						if strings.HasSuffix(ns.Name, "-leases") {
							return false, errTestClient
						}
					}
					return true, nil
				})
			},
			wantErr: `failed to create or apply role binding for leases: failed to check istio-test-ns/cert-manager-istio-csr-leases rolebinding resource already exists: test client error`,
		},
	}
	runRBACTests(t, tests)
}

func testCreateOrApplyRBACResource_ListingErrors(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "clusterrolebindings reconciliation fails while listing existing resources",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(_ context.Context, obj client.ObjectList, _ ...client.ListOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBindingList:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create or apply cluster role bindings: failed to find existing cluster role binding: failed to list clusterrolebinding resources, impacted namespace istiocsr-test-ns: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation fails while listing multiple existing resources",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(_ context.Context, obj client.ObjectList, _ ...client.ListOption) error {
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
			wantErr: `failed to create or apply cluster role bindings: failed to find existing cluster role binding: matched clusterrolebinding resources: [{TypeMeta:{Kind:ClusterRoleBinding APIVersion:rbac.authorization.k8s.io/v1} ObjectMeta:{Name:cert-manager-istio-csr-dfkhk GenerateName:cert-manager-istio-csr- Namespace: SelfLink: UID: ResourceVersion: Generation:0 CreationTimestamp:0001-01-01 00:00:00 +0000 UTC DeletionTimestamp:<nil> DeletionGracePeriodSeconds:<nil> Labels:map[app:cert-manager-istio-csr app.kubernetes.io/instance:cert-manager-istio-csr app.kubernetes.io/managed-by:cert-manager-operator app.kubernetes.io/name:cert-manager-istio-csr app.kubernetes.io/part-of:cert-manager-operator app.kubernetes.io/version:] Annotations:map[] OwnerReferences:[] Finalizers:[] ManagedFields:[]} Subjects:[{Kind:ServiceAccount APIGroup: Name:cert-manager-istio-csr Namespace:cert-manager}] RoleRef:{APIGroup:rbac.authorization.k8s.io Kind:ClusterRole Name:cert-manager-istio-csr}} {TypeMeta:{Kind:ClusterRoleBinding APIVersion:rbac.authorization.k8s.io/v1} ObjectMeta:{Name:cert-manager-istio-csr-dfmfj GenerateName:cert-manager-istio-csr- Namespace: SelfLink: UID: ResourceVersion: Generation:0 CreationTimestamp:0001-01-01 00:00:00 +0000 UTC DeletionTimestamp:<nil> DeletionGracePeriodSeconds:<nil> Labels:map[app:cert-manager-istio-csr app.kubernetes.io/instance:cert-manager-istio-csr app.kubernetes.io/managed-by:cert-manager-operator app.kubernetes.io/name:cert-manager-istio-csr app.kubernetes.io/part-of:cert-manager-operator app.kubernetes.io/version:] Annotations:map[] OwnerReferences:[] Finalizers:[] ManagedFields:[]} Subjects:[{Kind:ServiceAccount APIGroup: Name:cert-manager-istio-csr Namespace:cert-manager}] RoleRef:{APIGroup:rbac.authorization.k8s.io Kind:ClusterRole Name:cert-manager-istio-csr}}]: more than 1 clusterrolebinding resources exist with matching labels`,
		},
	}
	runRBACTests(t, tests)
}

func testCreateOrApplyRBACResource_ClusterRoleBinding(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "clusterrolebindings reconciliation successful",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(_ context.Context, obj client.ObjectList, _ ...client.ListOption) error {
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
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRoleBinding:
						clusterRoleBinding := testClusterRoleBinding()
						clusterRoleBinding.Labels = nil
						clusterRoleBinding.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						return errTestClient
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.ClusterRoleBinding = "cert-manager-istio-csr-dfkhk"
			},
			wantErr: `failed to create or apply cluster role bindings: failed to reconcile cluster role binding resource: failed to update /cert-manager-istio-csr-dfkhk clusterrolebinding resource: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation updating to desired state successful",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRoleBinding:
						clusterRoleBinding := testClusterRoleBinding()
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
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.ClusterRoleBinding:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create or apply cluster role bindings: failed to reconcile cluster role binding resource: failed to create  clusterrolebinding resource: test client error`,
		},
	}
	runRBACTests(t, tests)
}

func testCreateOrApplyRBACResource_ClusterRole(t *testing.T) {
	t.Run("status update errors", func(t *testing.T) {
		testCreateOrApplyRBACResource_ClusterRole_StatusUpdate(t)
	})
	t.Run("reconciliation errors", func(t *testing.T) {
		testCreateOrApplyRBACResource_ClusterRole_Reconciliation(t)
	})
}

func testCreateOrApplyRBACResource_ClusterRole_StatusUpdate(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "clusterrole reconciliation updating name in status fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(_ context.Context, obj client.ObjectList, _ ...client.ListOption) error {
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
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						clusterRole := testClusterRole()
						clusterRole.Labels = nil
						clusterRole.DeepCopyInto(o)
					}
					return true, nil
				})
				m.StatusUpdateCalls(func(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
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
			wantErr: `failed to create or apply cluster roles: failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with cert-manager-istio-csr-sdghj clusterrole resource name: failed to update status for "istiocsr-test-ns/istiocsr-test-resource": failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
		},
		{
			name: "clusterrolebindings reconciliation updating name in status fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(_ context.Context, obj client.ObjectList, _ ...client.ListOption) error {
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
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						clusterRole := testClusterRole()
						clusterRole.Labels = nil
						clusterRole.DeepCopyInto(o)
					}
					return true, nil
				})
				m.StatusUpdateCalls(func(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
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
			wantErr: `failed to create or apply cluster role bindings: failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with /cert-manager-istio-csr-dfkhk clusterrolebinding resource name: failed to update status for "istiocsr-test-ns/istiocsr-test-resource": failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
		},
	}
	runRBACTests(t, tests)
}

func testCreateOrApplyRBACResource_ClusterRole_Reconciliation(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "clusterrole reconciliation updating to desired state fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.ClusterRole:
						clusterRole := testClusterRole()
						clusterRole.Labels = nil
						clusterRole.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
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
			wantErr: `failed to create or apply cluster roles: failed to reconcile cluster role resource: failed to update /cert-manager-istio-csr-sdghj clusterrole resource: test client error`,
		},
		{
			name: "clusterrole reconciliation creation fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.ClusterRole:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.ClusterRole:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create or apply cluster roles: failed to reconcile cluster role resource: failed to create  clusterrole resource: test client error`,
		},
	}
	runRBACTests(t, tests)
}

func testCreateOrApplyRBACResource_RoleAndBinding(t *testing.T) {
	t.Run("role cases", func(t *testing.T) {
		testCreateOrApplyRBACResource_Role(t)
	})
	t.Run("role binding cases", func(t *testing.T) {
		testCreateOrApplyRBACResource_RoleBinding(t)
	})
}

func testCreateOrApplyRBACResource_Role(t *testing.T) {
	t.Run("regular role cases", func(t *testing.T) {
		testCreateOrApplyRBACResource_Role_Regular(t)
	})
	t.Run("leases role cases", func(t *testing.T) {
		testCreateOrApplyRBACResource_Role_Leases(t)
	})
}

func testCreateOrApplyRBACResource_Role_Regular(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "role reconciliation updating to desired state fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.Role:
						role := testRole()
						role.Labels = nil
						role.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.Role:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create or apply roles: failed to update istio-test-ns/cert-manager-istio-csr role resource: test client error`,
		},
		{
			name: "role reconciliation creation fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.Role:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.Role:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create or apply roles: failed to create istio-test-ns/cert-manager-istio-csr role resource: test client error`,
		},
	}
	runRBACTests(t, tests)
}

func testCreateOrApplyRBACResource_Role_Leases(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "role-leases reconciliation updating to desired state fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
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
				m.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.Role:
						if strings.HasSuffix(obj.GetName(), "-leases") {
							return errTestClient
						}
					}
					return nil
				})
			},
			wantErr: `failed to create or apply role for leases: failed to update istio-test-ns/cert-manager-istio-csr-leases role resource: test client error`,
		},
		{
			name: "role-leases reconciliation creation fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.Role:
						if strings.HasSuffix(ns.Name, "-leases") {
							return false, nil
						}
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.Role:
						if strings.HasSuffix(obj.GetName(), "-leases") {
							return errTestClient
						}
					}
					return nil
				})
			},
			wantErr: `failed to create or apply role for leases: failed to create istio-test-ns/cert-manager-istio-csr-leases role resource: test client error`,
		},
	}
	runRBACTests(t, tests)
}

func testCreateOrApplyRBACResource_RoleBinding(t *testing.T) {
	t.Run("regular rolebinding cases", func(t *testing.T) {
		testCreateOrApplyRBACResource_RoleBinding_Regular(t)
	})
	t.Run("leases rolebinding cases", func(t *testing.T) {
		testCreateOrApplyRBACResource_RoleBinding_Leases(t)
	})
}

func testCreateOrApplyRBACResource_RoleBinding_Regular(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "rolebindings reconciliation updating to desired state fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch o := object.(type) {
					case *rbacv1.RoleBinding:
						role := testRoleBinding()
						role.Labels = nil
						role.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create or apply role bindings: failed to update istio-test-ns/cert-manager-istio-csr rolebinding resource: test client error`,
		},
		{
			name: "rolebindings reconciliation creation fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.RoleBinding:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create or apply role bindings: failed to create istio-test-ns/cert-manager-istio-csr rolebinding resource: test client error`,
		},
	}
	runRBACTests(t, tests)
}

func testCreateOrApplyRBACResource_RoleBinding_Leases(t *testing.T) {
	tests := []struct {
		name                       string
		preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR             func(*v1alpha1.IstioCSR)
		wantClusterRoleBindingName string
		wantErr                    string
	}{
		{
			name: "rolebinding-leases reconciliation updating to desired state fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
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
				m.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						if strings.HasSuffix(obj.GetName(), "-leases") {
							return errTestClient
						}
					}
					return nil
				})
			},
			wantErr: `failed to create or apply role binding for leases: failed to update istio-test-ns/cert-manager-istio-csr-leases rolebinding resource: test client error`,
		},
		{
			name: "rolebinding-leases reconciliation creation fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, ns types.NamespacedName, object client.Object) (bool, error) {
					switch object.(type) {
					case *rbacv1.RoleBinding:
						if strings.HasSuffix(ns.Name, "-leases") {
							return false, nil
						}
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch obj.(type) {
					case *rbacv1.RoleBinding:
						if strings.HasSuffix(obj.GetName(), "-leases") {
							return errTestClient
						}
					}
					return nil
				})
			},
			wantErr: `failed to create or apply role binding for leases: failed to create istio-test-ns/cert-manager-istio-csr-leases rolebinding resource: test client error`,
		},
	}
	runRBACTests(t, tests)
}

// runRBACTests is a helper function to run RBAC test cases and reduce cyclomatic complexity.
func runRBACTests(t *testing.T, tests []struct {
	name                       string
	preReq                     func(*Reconciler, *fakes.FakeCtrlClient)
	updateIstioCSR             func(*v1alpha1.IstioCSR)
	wantClusterRoleBindingName string
	wantErr                    string
}) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.ctrlClient = mock
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
		})
	}
}
