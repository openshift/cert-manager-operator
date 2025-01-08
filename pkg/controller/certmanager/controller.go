/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package certmanager

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatoropenshiftiov1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// TODO: This is just a placeholder controller to contain all the required rbac
// in a single place. Needs to be deleted later.

// CertManagerReconciler reconciles a CertManager object
type CertManagerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=operator.openshift.io,resources=certmanagers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.openshift.io,resources=certmanagers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.openshift.io,resources=certmanagers/finalizers,verbs=update

// TODO clusterpermissions carried over as is, need to be reduced
//+kubebuilder:rbac:groups="",resources=pods;secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events;services;namespaces;serviceaccounts;configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="rbac.authorization.k8s.io",resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="apps",resources=deployments;replicasets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="config.openshift.io",resources=certmanagers;clusteroperators;clusteroperators/status;infrastructures,verbs=get;list;watch;create;update;patch;delete

//+kubebuilder:rbac:groups="cert-manager.io",resources=certificaterequests;certificaterequests/finalizers;certificaterequests/status;certificates;certificates/finalizers;certificates/status;clusterissuers;clusterissuers/status;issuers;issuers/status,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups="certificates.k8s.io",resources=certificatesigningrequests;certificatesigningrequests/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="certificates.k8s.io",resources=signers,verbs=get;list;watch;create;update;patch;delete;sign
//+kubebuilder:rbac:groups="cert-manager.io",resources=signers,resourceNames=clusterissuers.cert-manager.io/*;issuers.cert-manager.io/*,verbs=approve
//+kubebuilder:rbac:groups="gateway.networking.k8s.io",resources=gateways;gateways/finalizers;httproutes;httproutes/finalizers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses;ingresses/finalizers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="apiregistration.k8s.io",resources=apiservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="acme.cert-manager.io",resources=challenges;challenges/finalizers;challenges/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="acme.cert-manager.io",resources=challenges;challenges/finalizers;challenges/status;orders;orders/finalizers;orders/status,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups="route.openshift.io",resources=routes;routes/custom-host,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CertManager object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *CertManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CertManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatoropenshiftiov1alpha1.CertManager{}).
		Complete(r)
}
