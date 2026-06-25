package http01proxy

import (
	"context"
	"fmt"
	"maps"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyDaemonSet(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, resourceLabels map[string]string) error {
	desired, err := r.getDaemonSetObject(proxy, resourceLabels)
	if err != nil {
		return common.NewIrrecoverableError(err, "failed to build daemonset object")
	}

	if err := r.createOrUpdateResource(ctx, desired); err != nil {
		return err
	}

	r.updateImageInStatus(proxy, desired)
	return nil
}

func (r *Reconciler) getDaemonSetObject(proxy *v1alpha1.HTTP01Proxy, resourceLabels map[string]string) (*appsv1.DaemonSet, error) {
	ds := common.DecodeObjBytes[*appsv1.DaemonSet](codecs, appsv1.SchemeGroupVersion, assets.MustAsset(daemonsetAssetName))

	ds.SetNamespace(proxy.GetNamespace())
	common.UpdateResourceLabels(ds, resourceLabels)
	if ds.Spec.Template.Labels == nil {
		ds.Spec.Template.Labels = make(map[string]string)
	}
	maps.Copy(ds.Spec.Template.Labels, resourceLabels)

	if r.proxyImage == "" {
		return nil, fmt.Errorf("environment variable %s is not set", http01proxyImageNameEnvVarName)
	}
	if len(ds.Spec.Template.Spec.Containers) == 0 {
		return nil, fmt.Errorf("DaemonSet asset %s has no containers defined", daemonsetAssetName)
	}
	ds.Spec.Template.Spec.Containers[0].Image = r.proxyImage

	port := r.getInternalPort(proxy)
	r.updateDaemonSetPort(ds, port)

	return ds, nil
}

func (r *Reconciler) updateDaemonSetPort(ds *appsv1.DaemonSet, port int32) {
	container := &ds.Spec.Template.Spec.Containers[0]

	for i := range container.Ports {
		if container.Ports[i].Name == proxyPortName {
			container.Ports[i].ContainerPort = port
			container.Ports[i].HostPort = port
		}
	}

	portStr := strconv.FormatInt(int64(port), 10)
	envUpdated := false
	for i := range container.Env {
		if container.Env[i].Name == proxyPortEnvVar {
			container.Env[i].Value = portStr
			envUpdated = true
			break
		}
	}
	if !envUpdated {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  proxyPortEnvVar,
			Value: portStr,
		})
	}
}

func (r *Reconciler) updateImageInStatus(proxy *v1alpha1.HTTP01Proxy, ds *appsv1.DaemonSet) {
	if len(ds.Spec.Template.Spec.Containers) > 0 {
		proxy.Status.ProxyImage = ds.Spec.Template.Spec.Containers[0].Image
	}
}

func (r *Reconciler) deleteDaemonSet(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	return r.deleteIfExists(ctx, &appsv1.DaemonSet{}, client.ObjectKey{
		Namespace: proxy.GetNamespace(),
		Name:      http01proxyCommonName,
	})
}
