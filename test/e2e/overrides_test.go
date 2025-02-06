//go:build e2e
// +build e2e

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Overrides test", Ordered, func() {

	BeforeEach(func() {
		By("Reset cert-manager state")
		err := resetCertManagerState(context.TODO(), certmanageroperatorclient, loader)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for operator status to become available")
		err = verifyOperatorStatusCondition(certmanageroperatorclient,
			[]string{certManagerControllerDeploymentControllerName,
				certManagerWebhookDeploymentControllerName,
				certManagerCAInjectorDeploymentControllerName},
			validOperatorStatusConditions)
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	Context("When adding valid cert-manager controller override args", func() {

		It("should add the args to the cert-manager controller deployment", func() {

			By("Adding cert-manager controller override args to the cert-managaer operator object")
			args := []string{"--dns01-recursive-nameservers=10.10.10.10:53", "--dns01-recursive-nameservers-only", "--enable-certificate-owner-ref", "--v=3"}
			err := addOverrideArgs(certmanageroperatorclient, certmanagerControllerDeployment, args)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager controller status to become available")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerControllerDeploymentControllerName}, validOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the args to be added to the cert-manager controller deployment")
			err = verifyDeploymentArgs(k8sClientSet, certmanagerControllerDeployment, args, true)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding valid cert-manager webhook override args", func() {

		It("should add the args to the cert-manager webhook deployment", func() {

			By("Adding cert-manager webhook override args to the cert-managaer operator object")
			args := []string{"--v=3"}
			err := addOverrideArgs(certmanageroperatorclient, certmanagerWebhookDeployment, args)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager webhook controller status to become available")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerWebhookDeploymentControllerName}, validOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the args to be added to the cert-manager webhook deployment")
			err = verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment, args, true)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding valid cert-manager cainjector override args", func() {

		It("should add the args to the cert-manager cainjector deployment", func() {

			By("Adding cert-manager cainjector override args to the cert-managaer operator object")
			args := []string{"--v=3"}
			err := addOverrideArgs(certmanageroperatorclient, certmanagerCAinjectorDeployment, args)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager cainjector controller status to become available")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerCAInjectorDeploymentControllerName}, validOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the args to be added to the cert-manager cainjector deployment")
			err = verifyDeploymentArgs(k8sClientSet, certmanagerCAinjectorDeployment, args, true)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding invalid cert-manager controller override args", func() {

		It("should not add the args to the cert-manager controller deployment", func() {

			By("Adding cert-manager controller override args to the cert-managaer operator object")
			args := []string{"--invalid-args=foo"}
			err := addOverrideArgs(certmanageroperatorclient, certmanagerControllerDeployment, args)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager controller status to become degraded")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerControllerDeploymentControllerName}, invalidOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the args are not added to the cert-manager controller deployment")
			err = verifyDeploymentArgs(k8sClientSet, certmanagerControllerDeployment, args, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding invalid cert-manager webhook override args", func() {

		It("should not add the args to the cert-manager webhook deployment", func() {

			By("Adding cert-manager webhook override args to the cert-managaer operator object")
			args := []string{"--dns01-recursive-nameservers=10.10.10.10:53", "--dns01-recursive-nameservers-only", "--enable-certificate-owner-ref"}
			err := addOverrideArgs(certmanageroperatorclient, certmanagerWebhookDeployment, args)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager webhook controller status to become degraded")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerWebhookDeploymentControllerName}, invalidOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the args are not added to the cert-manager webhook deployment")
			err = verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment, args, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding invalid cert-manager cainjector override args", func() {

		It("should not add the args to the cert-manager cainjector deployment", func() {

			By("Adding cert-manager cainjector override args to the cert-managaer operator object")
			args := []string{"--dns01-recursive-nameservers=10.10.10.10:53", "--dns01-recursive-nameservers-only", "--enable-certificate-owner-ref"}
			err := addOverrideArgs(certmanageroperatorclient, certmanagerCAinjectorDeployment, args)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager cainjector controller status to become degraded")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerCAInjectorDeploymentControllerName}, invalidOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the args are not added to the cert-manager cainjector deployment")
			err = verifyDeploymentArgs(k8sClientSet, certmanagerCAinjectorDeployment, args, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding valid cert-manager controller override resources", func() {

		It("should add the resources to the cert-manager controller deployment", func() {

			By("Adding cert-manager controller override resources to the certmanager.operator object")
			res := v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("20m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
			}
			err := addOverrideResources(certmanageroperatorclient, certmanagerControllerDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager controller status to become available")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerControllerDeploymentControllerName}, validOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the resources to be added to the cert-manager controller deployment")
			err = verifyDeploymentResources(k8sClientSet, certmanagerControllerDeployment, res, true)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding valid cert-manager webhook override resources", func() {

		It("should add the resources to the cert-manager webhook deployment", func() {

			By("Adding cert-manager webhook override resources to the certmanager.operator object")
			res := v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("20m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
			}
			err := addOverrideResources(certmanageroperatorclient, certmanagerWebhookDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager webhook controller status to become available")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerWebhookDeploymentControllerName}, validOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the resources to be added to the cert-manager webhook deployment")
			err = verifyDeploymentResources(k8sClientSet, certmanagerWebhookDeployment, res, true)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding valid cert-manager cainjector override resources", func() {

		It("should add the resources to the cert-manager cainjector deployment", func() {

			By("Adding cert-manager cainjector override resources to the certmanager.operator object")
			res := v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("500m"),
					corev1.ResourceMemory: k8sresource.MustParse("128Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("20m"),
					corev1.ResourceMemory: k8sresource.MustParse("64Mi"),
				},
			}
			err := addOverrideResources(certmanageroperatorclient, certmanagerCAinjectorDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager cainjector controller status to become available")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerCAInjectorDeploymentControllerName}, validOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the resources to be added to the cert-manager cainjector deployment")
			err = verifyDeploymentResources(k8sClientSet, certmanagerCAinjectorDeployment, res, true)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding invalid cert-manager controller override resources", func() {

		It("should not add the resources to the cert-manager controller deployment", func() {

			By("Adding cert-manager controller override resources to the certmanager.operator object")
			res := v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("2Gi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("1Gi"),
				},
			}
			err := addOverrideResources(certmanageroperatorclient, certmanagerControllerDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager controller status to become degraded")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerControllerDeploymentControllerName}, invalidOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the resources are not added to the cert-manager controller deployment")
			err = verifyDeploymentResources(k8sClientSet, certmanagerControllerDeployment, res, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding invalid cert-manager webhook override resources", func() {

		It("should not add the resources to the cert-manager webhook deployment", func() {

			By("Adding cert-manager webhook override resources to the certmanager.operator object")
			res := v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("2Gi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("1Gi"),
				},
			}
			err := addOverrideResources(certmanageroperatorclient, certmanagerWebhookDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager webhook controller status to become degraded")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerWebhookDeploymentControllerName}, invalidOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the resources are not added to the cert-manager webhook deployment")
			err = verifyDeploymentResources(k8sClientSet, certmanagerWebhookDeployment, res, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding invalid cert-manager cainjector override resources", func() {

		It("should not add the resources to the cert-manager cainjector deployment", func() {

			By("Adding cert-manager cainjector override resources to the certmanager.operator object")
			res := v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("2Gi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("1Gi"),
				},
			}
			err := addOverrideResources(certmanageroperatorclient, certmanagerCAinjectorDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager cainjector controller status to become degraded")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerCAInjectorDeploymentControllerName}, invalidOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the resources are not added to the cert-manager cainjector deployment")
			err = verifyDeploymentResources(k8sClientSet, certmanagerCAinjectorDeployment, res, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding valid cert-manager controller override scheduling", func() {

		It("should add the scheduling to the cert-manager controller deployment", func() {

			By("Adding cert-manager controller override scheduling to the certmanager.operator object")
			res := v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"node-role.kubernetes.io/control-plane": "",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/master",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			}
			err := addOverrideScheduling(certmanageroperatorclient, certmanagerControllerDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager controller status to become available")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerControllerDeploymentControllerName}, validOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the scheduling to be added to the cert-manager controller deployment")
			err = verifyDeploymentScheduling(k8sClientSet, certmanagerControllerDeployment, res, true)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding valid cert-manager webhook override scheduling", func() {

		It("should add the scheduling to the cert-manager webhook deployment", func() {

			By("Adding cert-manager webhook override scheduling to the certmanager.operator object")
			res := v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"node-role.kubernetes.io/control-plane": "",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/master",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			}
			err := addOverrideScheduling(certmanageroperatorclient, certmanagerWebhookDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager webhook controller status to become available")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerWebhookDeploymentControllerName}, validOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the scheduling to be added to the cert-manager webhook deployment")
			err = verifyDeploymentScheduling(k8sClientSet, certmanagerWebhookDeployment, res, true)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding valid cert-manager cainjector override scheduling", func() {

		It("should add the scheduling to the cert-manager cainjector deployment", func() {

			By("Adding cert-manager cainjector override scheduling to the certmanager.operator object")
			res := v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"node-role.kubernetes.io/control-plane": "",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/master",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
				},
			}
			err := addOverrideScheduling(certmanageroperatorclient, certmanagerCAinjectorDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager cainjector controller status to become available")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerCAInjectorDeploymentControllerName}, validOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for the scheduling to be added to the cert-manager cainjector deployment")
			err = verifyDeploymentScheduling(k8sClientSet, certmanagerCAinjectorDeployment, res, true)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding invalid cert-manager controller override scheduling", func() {

		It("should not add the scheduling to the cert-manager controller deployment", func() {

			By("Adding cert-manager controller override scheduling to the certmanager.operator object")
			res := v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"node/Label/1": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				},
			}
			err := addOverrideScheduling(certmanageroperatorclient, certmanagerControllerDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager controller status to become degraded")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerControllerDeploymentControllerName}, invalidOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the scheduling are not added to the cert-manager controller deployment")
			err = verifyDeploymentScheduling(k8sClientSet, certmanagerControllerDeployment, res, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding invalid cert-manager webhook override scheduling", func() {

		It("should not add the scheduling to the cert-manager webhook deployment", func() {

			By("Adding cert-manager webhook override scheduling to the certmanager.operator object")
			res := v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"node/Label/1": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				},
			}
			err := addOverrideScheduling(certmanageroperatorclient, certmanagerWebhookDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager webhook controller status to become degraded")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerWebhookDeploymentControllerName}, invalidOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the scheduling are not added to the cert-manager webhook deployment")
			err = verifyDeploymentScheduling(k8sClientSet, certmanagerWebhookDeployment, res, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When adding invalid cert-manager cainjector override scheduling", func() {

		It("should not add the scheduling to the cert-manager cainjector deployment", func() {

			By("Adding cert-manager cainjector override scheduling to the certmanager.operator object")
			res := v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"node/Label/1": "value",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration",
						Operator: "Exists",
						Value:    "value",
						Effect:   "NoSchedule",
					},
				},
			}
			err := addOverrideScheduling(certmanageroperatorclient, certmanagerCAinjectorDeployment, res)
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for cert-manager cainjector controller status to become degraded")
			err = verifyOperatorStatusCondition(certmanageroperatorclient, []string{certManagerCAInjectorDeploymentControllerName}, invalidOperatorStatusConditions)
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the scheduling are not added to the cert-manager cainjector deployment")
			err = verifyDeploymentScheduling(k8sClientSet, certmanagerCAinjectorDeployment, res, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	AfterAll(func() {
		By("Reset cert-manager state")
		err := resetCertManagerState(context.TODO(), certmanageroperatorclient, loader)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for operator status to become available")
		err = verifyOperatorStatusCondition(certmanageroperatorclient,
			[]string{certManagerControllerDeploymentControllerName,
				certManagerWebhookDeploymentControllerName,
				certManagerCAInjectorDeploymentControllerName},
			validOperatorStatusConditions)
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})
})
