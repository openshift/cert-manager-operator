package certmanager

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	fakeclientset "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"
)

func TestCreateDefaultCertManager(t *testing.T) {
	tests := []struct {
		name     string
		existing []runtime.Object
		wantErr  bool
	}{
		{
			name: "creates managed cluster CertManager",
		},
		{
			name: "fails when CertManager already exists",
			existing: []runtime.Object{
				&v1alpha1.CertManager{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Spec: v1alpha1.CertManagerSpec{
						OperatorSpec: operatorv1.OperatorSpec{
							ManagementState: operatorv1.Managed,
						},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fakeclientset.NewSimpleClientset(tt.existing...)
			controller := &DefaultCertManagerController{
				certManagerClient: fakeClient.OperatorV1alpha1(),
			}

			ctx := context.Background()
			cm, err := controller.createDefaultCertManager(ctx)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cm)
			assert.Equal(t, "cluster", cm.Name)
			assert.Equal(t, operatorv1.Managed, cm.Spec.ManagementState)

			// Verify the resource was actually created in the fake client
			got, err := fakeClient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, "cluster", got.Name)
			assert.Equal(t, operatorv1.Managed, got.Spec.ManagementState)
		})
	}
}
