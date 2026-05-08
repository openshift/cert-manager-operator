package trustmanager

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func (r *Reconciler) reconcileTrustManagerDeployment(trustManager *v1alpha1.TrustManager) error {
	if err := validateTrustManagerConfig(trustManager); err != nil {
		return fmt.Errorf("%s configuration validation failed: %w", trustManager.GetName(), err)
	}

	// Merge user-provided labels with controller's own default labels.
	resourceLabels := make(map[string]string)
	if len(trustManager.Spec.ControllerConfig.Labels) != 0 {
		for k, v := range trustManager.Spec.ControllerConfig.Labels {
			resourceLabels[k] = v
		}
	}
	for k, v := range controllerDefaultResourceLabels {
		resourceLabels[k] = v
	}

	// Validate trust namespace exists
	trustNamespace := trustManager.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = operandNamespace
	}
	if err := r.validateNamespaceExists(trustNamespace); err != nil {
		return fmt.Errorf("trust namespace %q does not exist: %w", trustNamespace, err)
	}

	// Step 1: Create ServiceAccount
	if err := r.reconcileServiceAccount(trustManager, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile ServiceAccount")
		return err
	}

	// Step 2: Create RBAC resources
	if err := r.reconcileRBAC(trustManager, resourceLabels, trustNamespace); err != nil {
		r.log.Error(err, "failed to reconcile RBAC resources")
		return err
	}

	// Step 3: Create Certificate and Issuer for webhook TLS
	if err := r.reconcileCertificates(trustManager, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile Certificate resources")
		return err
	}

	// Step 4: Create Services
	if err := r.reconcileServices(trustManager, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile Service resources")
		return err
	}

	// Step 5: Handle DefaultCAPackage ConfigMap if enabled
	var caPackageHash string
	if trustManager.Spec.TrustManagerConfig.DefaultCAPackage.Policy == v1alpha1.DefaultCAPackagePolicyEnabled {
		hash, err := r.reconcileDefaultCAPackage(trustManager, resourceLabels)
		if err != nil {
			r.log.Error(err, "failed to reconcile DefaultCAPackage")
			return err
		}
		caPackageHash = hash
	}

	// Step 6: Create Deployment
	if err := r.reconcileDeployment(trustManager, resourceLabels, trustNamespace, caPackageHash); err != nil {
		r.log.Error(err, "failed to reconcile Deployment")
		return err
	}

	r.log.V(4).Info("finished reconciliation of trustmanager", "name", trustManager.GetName())
	return nil
}

func (r *Reconciler) validateNamespaceExists(namespace string) error {
	ns := &corev1.Namespace{}
	if err := r.Get(r.ctx, types.NamespacedName{Name: namespace}, ns); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("namespace %q does not exist, please create it before creating the TrustManager CR", namespace)
		}
		return fmt.Errorf("failed to check namespace %q: %w", namespace, err)
	}
	return nil
}

func validateTrustManagerConfig(trustManager *v1alpha1.TrustManager) error {
	if trustManager.Spec.TrustManagerConfig.SecretTargets.Policy == v1alpha1.SecretTargetsPolicyCustom {
		if len(trustManager.Spec.TrustManagerConfig.SecretTargets.AuthorizedSecrets) == 0 {
			return fmt.Errorf("spec.trustManagerConfig.secretTargets.authorizedSecrets must not be empty when policy is Custom")
		}
	}
	return nil
}

func (r *Reconciler) reconcileServiceAccount(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string) error {
	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName,
			Namespace: operandNamespace,
			Labels:    resourceLabels,
		},
	}

	existing := &corev1.ServiceAccount{}
	exists, err := r.Exists(r.ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if err != nil {
		return fmt.Errorf("failed to check ServiceAccount existence: %w", err)
	}

	if !exists {
		r.log.Info("creating ServiceAccount", "name", desired.Name, "namespace", desired.Namespace)
		if err := r.Create(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to create ServiceAccount: %w", err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created ServiceAccount %s/%s", desired.Namespace, desired.Name)
		return nil
	}

	if objectMetadataModified(desired, existing) {
		existing.Labels = desired.Labels
		r.log.Info("updating ServiceAccount", "name", desired.Name, "namespace", desired.Namespace)
		if err := r.Update(r.ctx, existing); err != nil {
			return fmt.Errorf("failed to update ServiceAccount: %w", err)
		}
	}

	return nil
}

func (r *Reconciler) reconcileRBAC(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustNamespace string) error {
	// ClusterRole with dynamic rules based on secretTargets
	clusterRoleRules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{"trust.cert-manager.io"},
			Resources: []string{"bundles"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"trust.cert-manager.io"},
			Resources: []string{"bundles/finalizers"},
			Verbs:     []string{"update"},
		},
		{
			APIGroups: []string{"trust.cert-manager.io"},
			Resources: []string{"bundles/status"},
			Verbs:     []string{"patch"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get", "list", "create", "update", "patch", "watch", "delete"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"events"},
			Verbs:     []string{"create", "patch"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}

	// Add secret write rules when secretTargets policy is Custom
	if trustManager.Spec.TrustManagerConfig.SecretTargets.Policy == v1alpha1.SecretTargetsPolicyCustom {
		clusterRoleRules = append(clusterRoleRules, rbacv1.PolicyRule{
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			ResourceNames: trustManager.Spec.TrustManagerConfig.SecretTargets.AuthorizedSecrets,
			Verbs:         []string{"create", "update", "patch", "delete"},
		})
	}

	desiredClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   trustManagerContainerName,
			Labels: resourceLabels,
		},
		Rules: clusterRoleRules,
	}

	if err := r.createOrUpdateClusterRole(desiredClusterRole, trustManager); err != nil {
		return fmt.Errorf("failed to reconcile ClusterRole: %w", err)
	}

	// ClusterRoleBinding
	desiredCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   trustManagerContainerName,
			Labels: resourceLabels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     trustManagerContainerName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      trustManagerContainerName,
				Namespace: operandNamespace,
			},
		},
	}

	if err := r.createOrUpdateClusterRoleBinding(desiredCRB, trustManager); err != nil {
		return fmt.Errorf("failed to reconcile ClusterRoleBinding: %w", err)
	}

	// Role in trust namespace for secret access
	desiredTrustRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName,
			Namespace: trustNamespace,
			Labels:    resourceLabels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	if err := r.createOrUpdateRole(desiredTrustRole, trustManager); err != nil {
		return fmt.Errorf("failed to reconcile Role in trust namespace: %w", err)
	}

	// RoleBinding in trust namespace
	desiredTrustRB := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName,
			Namespace: trustNamespace,
			Labels:    resourceLabels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     trustManagerContainerName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      trustManagerContainerName,
				Namespace: operandNamespace,
			},
		},
	}

	if err := r.createOrUpdateRoleBinding(desiredTrustRB, trustManager); err != nil {
		return fmt.Errorf("failed to reconcile RoleBinding in trust namespace: %w", err)
	}

	// Leader election Role in operand namespace
	desiredLeaderRole := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName + ":leaderelection",
			Namespace: operandNamespace,
			Labels:    resourceLabels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"get", "create", "update", "watch", "list"},
			},
		},
	}

	if err := r.createOrUpdateRole(desiredLeaderRole, trustManager); err != nil {
		return fmt.Errorf("failed to reconcile leader election Role: %w", err)
	}

	// Leader election RoleBinding in operand namespace
	desiredLeaderRB := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName + ":leaderelection",
			Namespace: operandNamespace,
			Labels:    resourceLabels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     trustManagerContainerName + ":leaderelection",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      trustManagerContainerName,
				Namespace: operandNamespace,
			},
		},
	}

	if err := r.createOrUpdateRoleBinding(desiredLeaderRB, trustManager); err != nil {
		return fmt.Errorf("failed to reconcile leader election RoleBinding: %w", err)
	}

	return nil
}

func (r *Reconciler) reconcileCertificates(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string) error {
	// Issuer for webhook TLS
	desiredIssuer := &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName,
			Namespace: operandNamespace,
			Labels:    resourceLabels,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}

	existingIssuer := &certmanagerv1.Issuer{}
	exists, err := r.Exists(r.ctx, types.NamespacedName{Name: desiredIssuer.Name, Namespace: desiredIssuer.Namespace}, existingIssuer)
	if err != nil {
		return fmt.Errorf("failed to check Issuer existence: %w", err)
	}
	if !exists {
		r.log.Info("creating Issuer", "name", desiredIssuer.Name)
		if err := r.Create(r.ctx, desiredIssuer); err != nil {
			return fmt.Errorf("failed to create Issuer: %w", err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created Issuer %s/%s", desiredIssuer.Namespace, desiredIssuer.Name)
	}

	// Certificate for webhook TLS
	desiredCert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName,
			Namespace: operandNamespace,
			Labels:    resourceLabels,
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: fmt.Sprintf("%s.%s.svc", trustManagerContainerName, operandNamespace),
			DNSNames: []string{
				fmt.Sprintf("%s.%s.svc", trustManagerContainerName, operandNamespace),
			},
			SecretName:           trustManagerContainerName + "-tls",
			RevisionHistoryLimit: int32Ptr(1),
			IssuerRef: certmanagermetav1.ObjectReference{
				Name: trustManagerContainerName,
				Kind: "Issuer",
			},
		},
	}

	existingCert := &certmanagerv1.Certificate{}
	exists, err = r.Exists(r.ctx, types.NamespacedName{Name: desiredCert.Name, Namespace: desiredCert.Namespace}, existingCert)
	if err != nil {
		return fmt.Errorf("failed to check Certificate existence: %w", err)
	}
	if !exists {
		r.log.Info("creating Certificate", "name", desiredCert.Name)
		if err := r.Create(r.ctx, desiredCert); err != nil {
			return fmt.Errorf("failed to create Certificate: %w", err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created Certificate %s/%s", desiredCert.Namespace, desiredCert.Name)
	}

	return nil
}

func (r *Reconciler) reconcileServices(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string) error {
	// Webhook service
	desiredService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName,
			Namespace: operandNamespace,
			Labels:    resourceLabels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": trustManagerCommonName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "webhook",
					Port:       443,
					TargetPort: intstr.FromInt32(webhookPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	existing := &corev1.Service{}
	exists, err := r.Exists(r.ctx, types.NamespacedName{Name: desiredService.Name, Namespace: desiredService.Namespace}, existing)
	if err != nil {
		return fmt.Errorf("failed to check Service existence: %w", err)
	}
	if !exists {
		r.log.Info("creating Service", "name", desiredService.Name)
		if err := r.Create(r.ctx, desiredService); err != nil {
			return fmt.Errorf("failed to create Service: %w", err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created Service %s/%s", desiredService.Namespace, desiredService.Name)
	}

	// Metrics service
	desiredMetricsService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName + "-metrics",
			Namespace: operandNamespace,
			Labels:    resourceLabels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app": trustManagerCommonName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "metrics",
					Port:       metricsPort,
					TargetPort: intstr.FromInt32(metricsPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	existingMetrics := &corev1.Service{}
	exists, err = r.Exists(r.ctx, types.NamespacedName{Name: desiredMetricsService.Name, Namespace: desiredMetricsService.Namespace}, existingMetrics)
	if err != nil {
		return fmt.Errorf("failed to check Metrics Service existence: %w", err)
	}
	if !exists {
		r.log.Info("creating Metrics Service", "name", desiredMetricsService.Name)
		if err := r.Create(r.ctx, desiredMetricsService); err != nil {
			return fmt.Errorf("failed to create Metrics Service: %w", err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created Service %s/%s", desiredMetricsService.Namespace, desiredMetricsService.Name)
	}

	return nil
}

func (r *Reconciler) reconcileDefaultCAPackage(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string) (string, error) {
	// Read the injected CA bundle from operator namespace
	caConfigMap := &corev1.ConfigMap{}
	if err := r.Get(r.ctx, types.NamespacedName{Name: trustedCABundleConfigMapName, Namespace: operatorNamespace}, caConfigMap); err != nil {
		if errors.IsNotFound(err) {
			return "", fmt.Errorf("trusted CA bundle ConfigMap %s/%s not found, waiting for CNO injection", operatorNamespace, trustedCABundleConfigMapName)
		}
		return "", fmt.Errorf("failed to get trusted CA bundle ConfigMap: %w", err)
	}

	caBundle, ok := caConfigMap.Data["ca-bundle.crt"]
	if !ok || caBundle == "" {
		return "", fmt.Errorf("trusted CA bundle ConfigMap %s/%s does not contain ca-bundle.crt or is empty, waiting for CNO injection", operatorNamespace, trustedCABundleConfigMapName)
	}

	// Format into trust-manager expected JSON format
	packageJSON := map[string]string{
		"name":    "cert-manager-package-openshift",
		"bundle":  caBundle,
		"version": caConfigMap.ResourceVersion,
	}
	packageBytes, err := json.Marshal(packageJSON)
	if err != nil {
		return "", fmt.Errorf("failed to marshal CA package JSON: %w", err)
	}

	// Compute hash of the package for deployment annotation
	hash := fmt.Sprintf("%x", sha256.Sum256(packageBytes))

	// Create or update the package ConfigMap in operand namespace
	desiredCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultCAPackageConfigMapName,
			Namespace: operandNamespace,
			Labels:    resourceLabels,
		},
		Data: map[string]string{
			caPackageJSONName: string(packageBytes),
		},
	}

	existingCM := &corev1.ConfigMap{}
	exists, err := r.Exists(r.ctx, types.NamespacedName{Name: desiredCM.Name, Namespace: desiredCM.Namespace}, existingCM)
	if err != nil {
		return "", fmt.Errorf("failed to check DefaultCAPackage ConfigMap existence: %w", err)
	}

	if !exists {
		r.log.Info("creating DefaultCAPackage ConfigMap", "name", desiredCM.Name, "namespace", desiredCM.Namespace)
		if err := r.Create(r.ctx, desiredCM); err != nil {
			return "", fmt.Errorf("failed to create DefaultCAPackage ConfigMap: %w", err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created DefaultCAPackage ConfigMap %s/%s", desiredCM.Namespace, desiredCM.Name)
	} else if configMapDataModified(desiredCM, existingCM) {
		existingCM.Data = desiredCM.Data
		existingCM.Labels = desiredCM.Labels
		r.log.Info("updating DefaultCAPackage ConfigMap", "name", desiredCM.Name, "namespace", desiredCM.Namespace)
		if err := r.Update(r.ctx, existingCM); err != nil {
			return "", fmt.Errorf("failed to update DefaultCAPackage ConfigMap: %w", err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Updated", "Updated DefaultCAPackage ConfigMap %s/%s", desiredCM.Namespace, desiredCM.Name)
	}

	return hash, nil
}

func (r *Reconciler) reconcileDeployment(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustNamespace string, caPackageHash string) error {
	image := os.Getenv(trustManagerImageNameEnvVarName)
	if image == "" {
		return fmt.Errorf("environment variable %s is not set", trustManagerImageNameEnvVarName)
	}

	// Build container args based on spec
	args := buildContainerArgs(trustManager, trustNamespace)

	// Build volumes and volume mounts
	volumes := []corev1.Volume{
		{
			Name: "tls",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: trustManagerContainerName + "-tls",
				},
			},
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "tls",
			MountPath: "/tls",
			ReadOnly:  true,
		},
	}

	// Add default CA package volume if enabled
	if trustManager.Spec.TrustManagerConfig.DefaultCAPackage.Policy == v1alpha1.DefaultCAPackagePolicyEnabled {
		volumes = append(volumes, corev1.Volume{
			Name: "default-ca-package",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: defaultCAPackageConfigMapName,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "default-ca-package",
			MountPath: caPackageMountPath,
			ReadOnly:  true,
		})
	}

	// Build resource requirements
	resourceRequirements := trustManager.Spec.TrustManagerConfig.Resources
	if resourceRequirements.Requests == nil && resourceRequirements.Limits == nil {
		// Set sensible defaults if none provided
		resourceRequirements = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		}
	}

	replicas := int32(1)
	podTemplateAnnotations := make(map[string]string)
	if caPackageHash != "" {
		podTemplateAnnotations[defaultCAPackageHashAnnotation] = caPackageHash
	}

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustManagerContainerName,
			Namespace: operandNamespace,
			Labels:    resourceLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": trustManagerCommonName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      resourceLabels,
					Annotations: podTemplateAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: trustManagerContainerName,
					NodeSelector:       trustManager.Spec.TrustManagerConfig.NodeSelector,
					Tolerations:        trustManager.Spec.TrustManagerConfig.Tolerations,
					Affinity:           trustManager.Spec.TrustManagerConfig.Affinity,
					Containers: []corev1.Container{
						{
							Name:            trustManagerContainerName,
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Args:            args,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: webhookPort,
								},
								{
									ContainerPort: metricsPort,
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Port: intstr.FromInt32(readinessProbePort),
										Path: "/readyz",
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							Resources:    resourceRequirements,
							VolumeMounts: volumeMounts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	existingDeploy := &appsv1.Deployment{}
	exists, err := r.Exists(r.ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existingDeploy)
	if err != nil {
		return fmt.Errorf("failed to check Deployment existence: %w", err)
	}

	if !exists {
		r.log.Info("creating Deployment", "name", desired.Name, "namespace", desired.Namespace)
		if err := r.Create(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to create Deployment: %w", err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Created", "Created Deployment %s/%s", desired.Namespace, desired.Name)
		return nil
	}

	if deploymentNeedsUpdate(desired, existingDeploy) {
		existingDeploy.Spec = desired.Spec
		existingDeploy.Labels = desired.Labels
		r.log.Info("updating Deployment", "name", desired.Name, "namespace", desired.Namespace)
		if err := r.Update(r.ctx, existingDeploy); err != nil {
			return fmt.Errorf("failed to update Deployment: %w", err)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Updated", "Updated Deployment %s/%s", desired.Namespace, desired.Name)
	}

	return nil
}

func buildContainerArgs(trustManager *v1alpha1.TrustManager, trustNamespace string) []string {
	args := []string{
		fmt.Sprintf("--log-format=%s", trustManager.Spec.TrustManagerConfig.LogFormat),
		fmt.Sprintf("--log-level=%d", trustManager.Spec.TrustManagerConfig.LogLevel),
		fmt.Sprintf("--metrics-port=%d", metricsPort),
		fmt.Sprintf("--readiness-probe-port=%d", readinessProbePort),
		"--readiness-probe-path=/readyz",
		fmt.Sprintf("--trust-namespace=%s", trustNamespace),
		"--webhook-host=0.0.0.0",
		fmt.Sprintf("--webhook-port=%d", webhookPort),
		"--webhook-certificate-dir=/tls",
	}

	if trustManager.Spec.TrustManagerConfig.SecretTargets.Policy == v1alpha1.SecretTargetsPolicyCustom {
		args = append(args, "--secret-targets-enabled=true")
	}

	if trustManager.Spec.TrustManagerConfig.DefaultCAPackage.Policy == v1alpha1.DefaultCAPackagePolicyEnabled {
		args = append(args, fmt.Sprintf("--default-package-location=%s/%s", caPackageMountPath, caPackageJSONName))
	}

	if trustManager.Spec.TrustManagerConfig.FilterExpiredCertificates == v1alpha1.FilterExpiredCertificatesPolicyEnabled {
		args = append(args, "--filter-expired-certificates=true")
	}

	return args
}

func deploymentNeedsUpdate(desired, existing *appsv1.Deployment) bool {
	if len(desired.Spec.Template.Spec.Containers) == 0 || len(existing.Spec.Template.Spec.Containers) == 0 {
		return true
	}

	desiredContainer := desired.Spec.Template.Spec.Containers[0]
	existingContainer := existing.Spec.Template.Spec.Containers[0]

	if desiredContainer.Image != existingContainer.Image {
		return true
	}

	if !stringSlicesEqual(desiredContainer.Args, existingContainer.Args) {
		return true
	}

	// Check pod template annotations (for CA package hash)
	desiredAnnotations := desired.Spec.Template.Annotations
	existingAnnotations := existing.Spec.Template.Annotations
	if desiredAnnotations == nil {
		desiredAnnotations = map[string]string{}
	}
	if existingAnnotations == nil {
		existingAnnotations = map[string]string{}
	}
	if desiredAnnotations[defaultCAPackageHashAnnotation] != existingAnnotations[defaultCAPackageHashAnnotation] {
		return true
	}

	return false
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aStr := strings.Join(a, ",")
	bStr := strings.Join(b, ",")
	return aStr == bStr
}

func int32Ptr(val int32) *int32 {
	return &val
}
