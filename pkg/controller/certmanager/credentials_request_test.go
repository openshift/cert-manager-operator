package certmanager

import (
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	configv1 "github.com/openshift/api/config/v1"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configlistersv1 "github.com/openshift/client-go/config/listers/config/v1"

	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

const testSecretName = "cloud-creds"

// The following helpers return the full err.Error() the hook produces for each failure mode
// (same NotFound GroupResource/name and fmt.Errorf wording as withCloudCredentials; test-only).
func expectedCloudSecretRetryingNotFound(secretName string) string {
	nf := apierrors.NewNotFound(corev1.Resource("secret"), secretName)
	return fmt.Errorf("(Retrying) cloud secret %q doesn't exist due to %w", secretName, nf).Error()
}

func expectedUnsupportedCloudProvider(platform configv1.PlatformType) string {
	return fmt.Errorf("unsupported cloud provider %q for mounting cloud credentials secret", platform).Error()
}

func expectedInfrastructureClusterNotFound() string {
	return apierrors.NewNotFound(configv1.Resource("infrastructure"), "cluster").Error()
}

func TestWithCloudCredentials(t *testing.T) {
	tests := []struct {
		name            string
		deploymentName  string
		secretName      string
		secretInStore   bool
		decoySecretOnly bool // lister has a different secret name so Get(secretName) fails (not brittle on tt.name)
		platformType    configv1.PlatformType
		wantErr         string // full hook err.Error(); empty => expect nil error
		wantVolumes     int
		wantMountPath   string
		wantAWSEnv      bool
		noInfra         bool // if true, infra indexer is left empty so Get("cluster") fails
	}{
		{
			name:           "non-controller deployment no-op",
			deploymentName: certmanagerWebhookDeployment,
			secretName:     testSecretName,
			platformType:   configv1.AWSPlatformType,
			wantVolumes:    0,
		},
		{
			name:           "empty secret name returns nil",
			deploymentName: certmanagerControllerDeployment,
			secretName:     "",
			platformType:   configv1.AWSPlatformType,
			wantVolumes:    0,
		},
		{
			name:            "secret not found returns retry error",
			deploymentName:  certmanagerControllerDeployment,
			secretName:      "missing-secret",
			secretInStore:   false,
			decoySecretOnly: true,
			platformType:    configv1.AWSPlatformType,
			wantErr:         expectedCloudSecretRetryingNotFound("missing-secret"),
			wantVolumes:     0,
		},
		{
			name:           "AWS adds volume, mount and env",
			deploymentName: certmanagerControllerDeployment,
			secretName:     testSecretName,
			secretInStore:  true,
			platformType:   configv1.AWSPlatformType,
			wantVolumes:    1,
			wantMountPath:  awsCredentialsDir,
			wantAWSEnv:     true,
		},
		{
			name:           "GCP adds volume and mount, no AWS env",
			deploymentName: certmanagerControllerDeployment,
			secretName:     testSecretName,
			secretInStore:  true,
			platformType:   configv1.GCPPlatformType,
			wantVolumes:    1,
			wantMountPath:  gcpCredentialsDir,
			wantAWSEnv:     false,
		},
		{
			name:           "unsupported platform returns error",
			deploymentName: certmanagerControllerDeployment,
			secretName:     testSecretName,
			secretInStore:  true,
			platformType:   configv1.PlatformType("Unsupported"),
			wantErr:        expectedUnsupportedCloudProvider(configv1.PlatformType("Unsupported")),
			wantVolumes:    0,
		},
		{
			name:           "infra not found returns error",
			deploymentName: certmanagerControllerDeployment,
			secretName:     testSecretName,
			secretInStore:  true,
			platformType:   configv1.AWSPlatformType,
			wantErr:        expectedInfrastructureClusterNotFound(),
			noInfra:        true,
			wantVolumes:    0,
		},
		{
			name:           "Azure platform is unsupported",
			deploymentName: certmanagerControllerDeployment,
			secretName:     testSecretName,
			secretInStore:  true,
			platformType:   configv1.AzurePlatformType,
			wantErr:        expectedUnsupportedCloudProvider(configv1.AzurePlatformType),
			wantVolumes:    0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var kubeClient *fake.Clientset
			if tt.secretInStore {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: tt.secretName, Namespace: operatorclient.TargetNamespace},
				}
				kubeClient = fake.NewSimpleClientset(secret)
			} else if tt.decoySecretOnly {
				kubeClient = fake.NewSimpleClientset(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: operatorclient.TargetNamespace},
				})
			} else {
				kubeClient = fake.NewSimpleClientset()
			}
			kubeInformers := informers.NewSharedInformerFactory(kubeClient, 0)
			if tt.secretInStore || tt.wantErr != "" {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: tt.secretName, Namespace: operatorclient.TargetNamespace},
				}
				if tt.decoySecretOnly {
					secret.Name = "other"
				}
				kubeInformers.Core().V1().Secrets().Informer().GetStore().Add(secret)
			}
			stopCh := make(chan struct{})
			defer close(stopCh)
			kubeInformers.Start(stopCh)
			if tt.secretInStore || tt.wantErr != "" {
				if !cache.WaitForCacheSync(stopCh, kubeInformers.Core().V1().Secrets().Informer().HasSynced) {
					t.Fatal("secret informer did not sync")
				}
			}

			infraIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			if !tt.noInfra {
				infra := &configv1.Infrastructure{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Status: configv1.InfrastructureStatus{
						PlatformStatus: &configv1.PlatformStatus{Type: tt.platformType},
					},
				}
				_ = infraIndexer.Add(infra)
			}
			infraInformer := &fakeInfrastructureInformer{lister: configlistersv1.NewInfrastructureLister(infraIndexer)}

			hook := withCloudCredentials(
				kubeInformers.Core().V1().Secrets(),
				infraInformer,
				tt.deploymentName,
				tt.secretName,
			)
			deployment := &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "controller"}},
						},
					},
				},
			}
			err := hook(nil, deployment)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if got := err.Error(); got != tt.wantErr {
					t.Errorf("error = %q, want %q", got, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n := len(deployment.Spec.Template.Spec.Volumes); n != tt.wantVolumes {
				t.Errorf("volumes count = %d, want %d", n, tt.wantVolumes)
			}
			if tt.wantVolumes > 0 {
				if deployment.Spec.Template.Spec.Volumes[0].Name != cloudCredentialsVolumeName {
					t.Errorf("volume name = %q, want %q", deployment.Spec.Template.Spec.Volumes[0].Name, cloudCredentialsVolumeName)
				}
				if tt.wantMountPath != "" {
					mounts := deployment.Spec.Template.Spec.Containers[0].VolumeMounts
					if len(mounts) == 0 {
						t.Fatalf("expected VolumeMount for mount path %q, got none (containers[0].VolumeMounts is empty)", tt.wantMountPath)
					}
					if mounts[0].MountPath != tt.wantMountPath {
						t.Errorf("mount path = %q, want %q", mounts[0].MountPath, tt.wantMountPath)
					}
				}
				var hasAWS bool
				for _, e := range deployment.Spec.Template.Spec.Containers[0].Env {
					if e.Name == "AWS_SDK_LOAD_CONFIG" {
						hasAWS = true
						break
					}
				}
				if hasAWS != tt.wantAWSEnv {
					t.Errorf("AWS_SDK_LOAD_CONFIG present = %v, want %v", hasAWS, tt.wantAWSEnv)
				}
			}
		})
	}
}

// fakeInfrastructureInformer implements configinformersv1.InfrastructureInformer using a fixed lister.
type fakeInfrastructureInformer struct {
	lister configlistersv1.InfrastructureLister
}

func (f *fakeInfrastructureInformer) Informer() cache.SharedIndexInformer {
	return nil
}

func (f *fakeInfrastructureInformer) Lister() configlistersv1.InfrastructureLister {
	return f.lister
}

var _ configinformersv1.InfrastructureInformer = (*fakeInfrastructureInformer)(nil)
