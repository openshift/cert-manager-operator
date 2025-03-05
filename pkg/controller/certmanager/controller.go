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
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/features"

	"github.com/go-logr/logr"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

var (
	ControllerName = "cert-manager-ctrl-controller"

	reQInterval = 1 * time.Minute
)

// CertManagerReconciler reconciles a CertManager object
type CertManagerReconciler struct {
	client.Client

	scheme          *runtime.Scheme
	clock           clock.Clock
	log             logr.Logger
	featureAccessor features.FeatureAccessor
}

func NewCertManagerReconciler(mgr ctrl.Manager, featureAccessor features.FeatureAccessor) *CertManagerReconciler {
	return &CertManagerReconciler{
		Client: mgr.GetClient(),

		clock:           clock.RealClock{},
		log:             ctrl.Log.WithName(ControllerName),
		scheme:          mgr.GetScheme(),
		featureAccessor: featureAccessor,
	}
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

// Reconcile is run each time the CertManager object has a create/update/delete event.
// All objects other than certmanager.operator.openshift.io/cluster lead to no-op syncs.
func (r *CertManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.V(2).Info("reconciling", "request", req)

	if req.Name != "cluster" {
		r.log.V(2).Info("skipping reconciliation for object certmanager.openshift.operator.io", "name", req.Name)
		return ctrl.Result{}, nil
	}

	cm := &v1alpha1.CertManager{}
	if err := r.Get(ctx, req.NamespacedName, cm); err != nil {
		if errors.IsNotFound(err) {
			// NotFound errors, since they can't be fixed by an immediate
			// requeue (have to wait for a new notification), and can be processed
			// on deleted requests.
			r.log.V(2).Info("certmanager.openshift.operator.io object not found, skipping reconciliation", "request", req)
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to fetch certmanager.openshift.operator.io %q during reconciliation: %w", req.NamespacedName, err)
	}

	var observedTechPreview bool
	if cm.Spec.UnsupportedFeatures != nil && len(cm.Spec.UnsupportedFeatures.TechPreview) > 0 {
		observedTechPreview = true

		// updates the list of runtime features with what was observed from the resource
		// Features once enabled, cannot be disabled: by design; thread-safe, idempotent.
		go func(tpFeatures v1alpha1.TechPreview) {
			err := r.featureAccessor.EnableMultipleFeatures(tpFeatures)
			if err != nil {
				err = fmt.Errorf("enabling feature gates from spec.unsupportedFeatures.techPreview failed: %v", err)
				r.log.V(3).Error(err, "techPreview", tpFeatures)
			}
		}(cm.Spec.UnsupportedFeatures.TechPreview.DeepCopy())
	}

	if alreadyInDesiredTPConditionState(cm, observedTechPreview) {
		return ctrl.Result{}, nil
	}

	_, err := r.setStatusWithTPCondition(ctx, cm, observedTechPreview)
	if err != nil {
		return ctrl.Result{RequeueAfter: reQInterval}, fmt.Errorf("failed to reconcile %q: %w", req, err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CertManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		For(&v1alpha1.CertManager{}).
		Complete(r)
}
