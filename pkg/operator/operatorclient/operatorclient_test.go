package operatorclient

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
)

func TestGetOperatorStateDefaultsUnknownManagementStateToManaged(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	fakeClient := fake.NewClientset(&v1alpha1.CertManager{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: v1alpha1.CertManagerSpec{
			OperatorSpec: operatorv1.OperatorSpec{
				LogLevel: operatorv1.Normal,
			},
		},
	})
	informers := certmanoperatorinformers.NewSharedInformerFactory(fakeClient, 0)
	informer := informers.Operator().V1alpha1().CertManagers()
	go informer.Informer().Run(ctx.Done())
	require.True(t, cache.WaitForCacheSync(ctx.Done(), informer.Informer().HasSynced))

	client := OperatorClient{
		Informers: informers,
		Client:    fakeClient.OperatorV1alpha1(),
		Clock:     clock.RealClock{},
	}

	spec, _, _, err := client.GetOperatorState()
	require.NoError(t, err)
	require.Equal(t, operatorv1.Managed, spec.ManagementState)
}

func TestGetOperatorStatePreservesExplicitManagementState(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	fakeClient := fake.NewClientset(&v1alpha1.CertManager{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: v1alpha1.CertManagerSpec{
			OperatorSpec: operatorv1.OperatorSpec{
				ManagementState: operatorv1.Unmanaged,
			},
		},
	})
	informers := certmanoperatorinformers.NewSharedInformerFactory(fakeClient, 0)
	informer := informers.Operator().V1alpha1().CertManagers()
	go informer.Informer().Run(ctx.Done())
	require.True(t, cache.WaitForCacheSync(ctx.Done(), informer.Informer().HasSynced))

	client := OperatorClient{
		Informers: informers,
		Client:    fakeClient.OperatorV1alpha1(),
		Clock:     clock.RealClock{},
	}

	spec, _, _, err := client.GetOperatorState()
	require.NoError(t, err)
	require.Equal(t, operatorv1.Unmanaged, spec.ManagementState)
}

func TestGetOperatorStateWithQuorumDefaultsUnknownManagementStateToManaged(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), wait.ForeverTestTimeout)
	t.Cleanup(cancel)

	fakeClient := fake.NewClientset(&v1alpha1.CertManager{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       v1alpha1.CertManagerSpec{},
	})
	client := OperatorClient{
		Client: fakeClient.OperatorV1alpha1(),
		Clock:  clock.RealClock{},
	}

	spec, _, _, err := client.GetOperatorStateWithQuorum(ctx)
	require.NoError(t, err)
	require.Equal(t, operatorv1.Managed, spec.ManagementState)
}
