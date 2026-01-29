package deployment

import (
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	coreinformersv1 "k8s.io/client-go/informers/core/v1"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"

	operatorv1 "github.com/openshift/api/operator/v1"
)

var (
	errUnsupportedCloudProvider = errors.New("unsupported cloud provider for mounting cloud credentials secret")
)

const (
	// credentials for AWS.
	awsCredentialsDir = "/.aws"

	// credentials for GCP.
	gcpCredentialsDir       = "/.config/gcloud"
	gcpCredentialsFileName  = "application_default_credentials.json"
	gcpCredentialsSecretKey = "service_account.json"

	// cloudCredentialsVolumeName is the volume name for mounting
	// service account (gcp) or credentials (aws) file.
	cloudCredentialsVolumeName = "cloud-credentials"
)

func withCloudCredentials(secretsInformer coreinformersv1.SecretInformer, infraInformer configinformersv1.InfrastructureInformer, deploymentName, secretName string) func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
	// cloud credentials is only required for the controller deployment,
	// other deployments should be left untouched
	if deploymentName != certmanagerControllerDeployment {
		return func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
			return nil
		}
	}

	return func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		if len(secretName) == 0 {
			return nil
		}

		_, err := secretsInformer.Lister().Secrets(operatorclient.TargetNamespace).Get(secretName)
		if err != nil && apierrors.IsNotFound(err) {
			return fmt.Errorf("(Retrying) cloud secret %q doesn't exist due to %w", secretName, err)
		} else if err != nil {
			return err
		}

		infra, err := infraInformer.Lister().Get("cluster")
		if err != nil {
			return err
		}

		var volume *corev1.Volume
		var volumeMount *corev1.VolumeMount
		var envVar *corev1.EnvVar

		switch infra.Status.PlatformStatus.Type {
		// supported cloud platform for mounting secrets
		case configv1.AWSPlatformType:
			volume = &corev1.Volume{
				Name: cloudCredentialsVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secretName,
					},
				},
			}
			volumeMount = &corev1.VolumeMount{
				Name:      cloudCredentialsVolumeName,
				MountPath: awsCredentialsDir,
			}

			// this is required as without this env var, aws sdk
			// doesn't properly bind role_arn from credentials file
			envVar = &corev1.EnvVar{
				Name:  "AWS_SDK_LOAD_CONFIG",
				Value: "1",
			}

		case configv1.GCPPlatformType:
			volume = &corev1.Volume{
				Name: cloudCredentialsVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secretName,
						Items: []corev1.KeyToPath{{
							Key:  gcpCredentialsSecretKey,
							Path: gcpCredentialsFileName,
						}},
					},
				},
			}
			volumeMount = &corev1.VolumeMount{
				Name:      cloudCredentialsVolumeName,
				MountPath: gcpCredentialsDir,
			}

		default:
			return fmt.Errorf("%w: %q", errUnsupportedCloudProvider, infra.Status.PlatformStatus.Type)
		}

		deployment.Spec.Template.Spec.Volumes = append(
			deployment.Spec.Template.Spec.Volumes,
			*volume,
		)
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
			*volumeMount,
		)

		if envVar != nil {
			deployment.Spec.Template.Spec.Containers[0].Env = append(
				deployment.Spec.Template.Spec.Containers[0].Env,
				*envVar,
			)
		}

		return nil
	}
}
