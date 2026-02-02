package deployment

import (
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

func TestVerifyCloudSecretExists(t *testing.T) {
	tests := []struct {
		name       string
		secretName string
		secrets    []runtime.Object
		wantErr    error
	}{
		{
			name:       "secret exists",
			secretName: "test-secret",
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: operatorclient.TargetNamespace,
					},
				},
			},
			wantErr: nil,
		},
		{
			name:       "secret not found - returns errCloudSecretNotFound",
			secretName: "missing-secret",
			secrets:    []runtime.Object{},
			wantErr:    errCloudSecretNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.secrets...)
			informerFactory := coreinformers.NewSharedInformerFactory(client, 0)
			informer := informerFactory.Core().V1().Secrets()

			// Add secrets to the informer cache
			for _, obj := range tt.secrets {
				if err := informer.Informer().GetStore().Add(obj); err != nil {
					t.Fatalf("failed to add secret to informer: %v", err)
				}
			}

			err := verifyCloudSecretExists(informer, tt.secretName)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("expected error containing %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error to wrap %v, got %v", tt.wantErr, err)
				}
			} else if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestApplyCloudCredentialsToDeployment(t *testing.T) {
	tests := []struct {
		name        string
		deployment  *appsv1.Deployment
		volume      *corev1.Volume
		volumeMount *corev1.VolumeMount
		envVar      *corev1.EnvVar
		wantErr     error
	}{
		{
			name: "successfully applies credentials to deployment",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test-container"},
							},
						},
					},
				},
			},
			volume: &corev1.Volume{
				Name: cloudCredentialsVolumeName,
			},
			volumeMount: &corev1.VolumeMount{
				Name:      cloudCredentialsVolumeName,
				MountPath: awsCredentialsDir,
			},
			envVar: &corev1.EnvVar{
				Name:  "AWS_SDK_LOAD_CONFIG",
				Value: "1",
			},
			wantErr: nil,
		},
		{
			name: "deployment has no containers - returns errDeploymentHasNoContainers",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-ns",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{},
						},
					},
				},
			},
			volume: &corev1.Volume{
				Name: cloudCredentialsVolumeName,
			},
			volumeMount: &corev1.VolumeMount{
				Name:      cloudCredentialsVolumeName,
				MountPath: awsCredentialsDir,
			},
			envVar:  nil,
			wantErr: errDeploymentHasNoContainers,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := applyCloudCredentialsToDeployment(tt.deployment, tt.volume, tt.volumeMount, tt.envVar)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("expected error containing %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error to wrap %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
					return
				}

				// Verify volume was added
				if len(tt.deployment.Spec.Template.Spec.Volumes) != 1 {
					t.Errorf("expected 1 volume, got %d", len(tt.deployment.Spec.Template.Spec.Volumes))
				}

				// Verify volume mount was added
				if len(tt.deployment.Spec.Template.Spec.Containers[0].VolumeMounts) != 1 {
					t.Errorf("expected 1 volume mount, got %d", len(tt.deployment.Spec.Template.Spec.Containers[0].VolumeMounts))
				}

				// Verify env var was added if provided
				if tt.envVar != nil {
					if len(tt.deployment.Spec.Template.Spec.Containers[0].Env) != 1 {
						t.Errorf("expected 1 env var, got %d", len(tt.deployment.Spec.Template.Spec.Containers[0].Env))
					}
				}
			}
		})
	}
}

func TestCreateCloudCredentialsResources(t *testing.T) {
	tests := []struct {
		name         string
		platformType configv1.PlatformType
		secretName   string
		wantErr      error
		checkVolume  func(*testing.T, *corev1.Volume)
		checkEnvVar  func(*testing.T, *corev1.EnvVar)
	}{
		{
			name:         "AWS platform creates correct resources",
			platformType: configv1.AWSPlatformType,
			secretName:   "aws-creds",
			wantErr:      nil,
			checkVolume: func(t *testing.T, vol *corev1.Volume) {
				if vol.Name != cloudCredentialsVolumeName {
					t.Errorf("expected volume name %s, got %s", cloudCredentialsVolumeName, vol.Name)
				}
				if vol.VolumeSource.Secret.SecretName != "aws-creds" {
					t.Errorf("expected secret name aws-creds, got %s", vol.VolumeSource.Secret.SecretName)
				}
			},
			checkEnvVar: func(t *testing.T, env *corev1.EnvVar) {
				if env == nil {
					t.Error("expected env var for AWS, got nil")
					return
				}
				if env.Name != "AWS_SDK_LOAD_CONFIG" {
					t.Errorf("expected env var name AWS_SDK_LOAD_CONFIG, got %s", env.Name)
				}
			},
		},
		{
			name:         "GCP platform creates correct resources",
			platformType: configv1.GCPPlatformType,
			secretName:   "gcp-creds",
			wantErr:      nil,
			checkVolume: func(t *testing.T, vol *corev1.Volume) {
				if vol.Name != cloudCredentialsVolumeName {
					t.Errorf("expected volume name %s, got %s", cloudCredentialsVolumeName, vol.Name)
				}
				if vol.VolumeSource.Secret.SecretName != "gcp-creds" {
					t.Errorf("expected secret name gcp-creds, got %s", vol.VolumeSource.Secret.SecretName)
				}
			},
			checkEnvVar: func(t *testing.T, env *corev1.EnvVar) {
				if env != nil {
					t.Errorf("expected no env var for GCP, got %+v", env)
				}
			},
		},
		{
			name:         "unsupported platform returns error",
			platformType: configv1.AzurePlatformType,
			secretName:   "azure-creds",
			wantErr:      errUnsupportedCloudProvider,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			volume, volumeMount, envVar, err := createCloudCredentialsResources(tt.platformType, tt.secretName)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("expected error %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error to wrap %v, got %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
					return
				}

				if tt.checkVolume != nil {
					tt.checkVolume(t, volume)
				}
				if tt.checkEnvVar != nil {
					tt.checkEnvVar(t, envVar)
				}
				if volumeMount == nil {
					t.Error("expected volume mount, got nil")
				}
			}
		})
	}
}

func TestErrorWrapping(t *testing.T) {
	// Test that verifyCloudSecretExists properly wraps errors with errCloudSecretNotFound
	client := fake.NewSimpleClientset()
	informerFactory := coreinformers.NewSharedInformerFactory(client, 0)
	informer := informerFactory.Core().V1().Secrets()

	err := verifyCloudSecretExists(informer, "nonexistent")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify error can be unwrapped to errCloudSecretNotFound
	if !errors.Is(err, errCloudSecretNotFound) {
		t.Errorf("expected error to wrap errCloudSecretNotFound, got %v", err)
	}

	// Verify error message contains the secret name
	errMsg := err.Error()
	if !strings.Contains(errMsg, "nonexistent") {
		t.Errorf("expected error message to contain secret name, got: %v", errMsg)
	}

	// Verify error message has the retrying prefix
	if !strings.Contains(errMsg, "(Retrying)") {
		t.Errorf("expected error message to contain (Retrying), got: %v", errMsg)
	}
}
