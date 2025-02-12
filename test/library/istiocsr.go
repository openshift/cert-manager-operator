package library

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientscheme "k8s.io/client-go/kubernetes/scheme"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

var (
	Scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientscheme.AddToScheme(Scheme))
	utilruntime.Must(appsv1.AddToScheme(Scheme))
	utilruntime.Must(corev1.AddToScheme(Scheme))
	utilruntime.Must(rbacv1.AddToScheme(Scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(Scheme))
	utilruntime.Must(v1alpha1.AddToScheme(Scheme))
}
