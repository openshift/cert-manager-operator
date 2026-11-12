package istiocsr

import (
	"context"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func testRESTMapper() meta.RESTMapper {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		certmanagerv1.SchemeGroupVersion,
		{Group: "cas-issuer.jetstack.io", Version: "v1"},
	})
	mapper.Add(certmanagerv1.SchemeGroupVersion.WithKind(certmanagerv1.IssuerKind), meta.RESTScopeNamespace)
	mapper.Add(certmanagerv1.SchemeGroupVersion.WithKind(certmanagerv1.ClusterIssuerKind), meta.RESTScopeRoot)
	mapper.Add(schema.GroupVersion{Group: "cas-issuer.jetstack.io", Version: "v1"}.WithKind("GoogleCASIssuer"), meta.RESTScopeNamespace)
	return mapper
}

type fakeAPIReader struct {
	client *fakes.FakeCtrlClient
}

func (f *fakeAPIReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	return f.client.Get(ctx, key, obj)
}

func (f *fakeAPIReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return f.client.List(ctx, list, opts...)
}

func attachTestIssuerClient(r *Reconciler, mock *fakes.FakeCtrlClient, istiocsr *v1alpha1.IstioCSR, userGet func(context.Context, client.ObjectKey, client.Object) error) {
	r.CtrlClient = mock
	r.apiReader = &fakeAPIReader{client: mock}
	r.restMapper = testRESTMapper()

	mock.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if handled, err := serveFakeIssuerUnstructuredGet(key, obj, istiocsr, userGet); handled {
			return err
		}
		if userGet != nil {
			return userGet(ctx, key, obj)
		}
		return nil
	})
}

func serveFakeIssuerUnstructuredGet(
	key client.ObjectKey,
	obj client.Object,
	istiocsr *v1alpha1.IstioCSR,
	userGet func(context.Context, client.ObjectKey, client.Object) error,
) (bool, error) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return false, nil
	}

	ref := resolvedIssuerRef(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef)
	switch strings.ToLower(ref.Kind) {
	case issuerKind:
		typed := &certmanagerv1.Issuer{}
		if userGet != nil {
			if err := userGet(context.Background(), key, typed); err != nil {
				return true, err
			}
		}
		return true, setUnstructuredFromTyped(u, typed, certmanagerv1.SchemeGroupVersion.WithKind(ref.Kind))
	case clusterIssuerKind:
		typed := &certmanagerv1.ClusterIssuer{}
		clusterKey := client.ObjectKey{Name: ref.Name}
		if userGet != nil {
			if err := userGet(context.Background(), clusterKey, typed); err != nil {
				// Preserve historical behavior where tests stub Get with the namespaced key.
				if err := userGet(context.Background(), key, typed); err != nil {
					return true, err
				}
			}
		}
		return true, setUnstructuredFromTyped(u, typed, certmanagerv1.SchemeGroupVersion.WithKind(ref.Kind))
	default:
		if userGet == nil {
			return true, apierrors.NewNotFound(schema.GroupResource{Group: ref.Group, Resource: strings.ToLower(ref.Kind)}, ref.Name)
		}
		external := &unstructured.Unstructured{}
		external.SetGroupVersionKind(schema.GroupVersionKind{Group: ref.Group, Version: "v1", Kind: ref.Kind})
		if err := userGet(context.Background(), key, external); err != nil {
			return true, err
		}
		u.SetUnstructuredContent(external.Object)
		u.SetGroupVersionKind(external.GroupVersionKind())
		return true, nil
	}
}

func setUnstructuredFromTyped(u *unstructured.Unstructured, src runtime.Object, gvk schema.GroupVersionKind) error {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(src)
	if err != nil {
		return err
	}
	u.SetUnstructuredContent(content)
	u.SetGroupVersionKind(gvk)
	return nil
}
