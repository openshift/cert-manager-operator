## Enabling metrics and monitoring for `cert-manager`

Cert-Manager exposes controller metrics in the format expected by [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator). 

ServiceMonitor resource needs to be created to scrape metrics from cert-manager operand, make sure Prometheus Operator is configured with required selectors.

`.spec.serviceMonitorNamespaceSelector` and `.spec.serviceMonitorSelector` fields of prometheus resource should contain corresponding `matchLabels: openshift.io/cluster-monitoring:true`. To verify it, we can run the following commands.

```sh
kubectl -n monitoring get prometheus k8s --template='{{.spec.serviceMonitorNamespaceSelector}}{{"\n"}}{{.spec.serviceMonitorSelector}}{{"\n"}}'
map[matchLabels:map[openshift.io/cluster-monitoring:true]]
map[]
```
For OpenShift:
```sh
oc -n openshift-monitoring get prometheus k8s --template='{{.spec.serviceMonitorNamespaceSelector}}{{"\n"}}{{.spec.serviceMonitorSelector}}{{"\n"}}'
map[matchLabels:map[openshift.io/cluster-monitoring:true]]
map[]
```
Label the Operand's namespace to enable cluster monitoring in it's namespace.

`
$ oc label namespace cert-manager openshift.io/cluster-monitoring=true
`

Please follow the steps below to `enable the monitoring for user-defined projects` in Openshift:

Cluster administrators can enable monitoring for user-defined projects by setting the `enableUserWorkload: true` field in the cluster monitoring ConfigMap object.

1. Edit the cluster-monitoring-config ConfigMap object:

`$ oc -n openshift-monitoring edit configmap cluster-monitoring-config`

2. Add `enableUserWorkload: true` under data/config.yaml:

```
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-monitoring-config
  namespace: openshift-monitoring
data:
  config.yaml: |
    enableUserWorkload: true
```

3. Check that the prometheus-operator, prometheus-user-workload and thanos-ruler-user-workload pods are running in the openshift-user-workload-monitoring project. It might take a short while for the pods to start:

`$ oc -n openshift-user-workload-monitoring get pod`
```
Example output

NAME                                   READY   STATUS        RESTARTS   AGE
prometheus-operator-6f7b748d5b-t7nbg   2/2     Running       0          3h
prometheus-user-workload-0             4/4     Running       1          3h
prometheus-user-workload-1             4/4     Running       1          3h
thanos-ruler-user-workload-0           3/3     Running       0          3h
thanos-ruler-user-workload-1           3/3     Running       0          3h
```
When set to true, the enableUserWorkload parameter enables monitoring for user-defined projects in a cluster.

For more details, Please look at the detailed documentation to [enable the monitoring for user-defined projects in Openshift](https://docs.openshift.com/container-platform/4.11/monitoring/enabling-monitoring-for-user-defined-projects.html):

4. Apply the Service Monitor in your openshift cluster.

`service-monitor.yaml`
```
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    app: cert-manager
    app.kubernetes.io/component: controller
    app.kubernetes.io/instance: cert-manager
    app.kubernetes.io/name: cert-manager
  name: cert-manager
  namespace: cert-manager
spec:
  endpoints:
  - interval: 30s
    port: tcp-prometheus-servicemonitor
    scheme: http
  selector:
    matchLabels:
      app.kubernetes.io/component: controller
      app.kubernetes.io/instance: cert-manager
      app.kubernetes.io/name: cert-manager
```
`$ oc apply -f service-monitor.yaml -n cert-manager`

The 'Service Monitor' will be collecting the metrics through the cert-manager `service` and will be using the port name of the service as its endpoints port. 
Following [Template](https://github.com/cert-manager/cert-manager/blob/master/deploy/charts/cert-manager/templates/servicemonitor.yaml) can be used for the helm configurations.

### Quering Metrics

As a cluster administrator or as a user with view permissions for all projects, you can access metrics for all default OpenShift Container Platform and user-defined projects in the Metrics UI by using the endpoints of the `cert-manager service`.

`$ oc describe service cert-manager -n cert-manager`

To query cert-manager controller metrics, select `Observe → Metrics` and filter the metrics of the cert-manager controller with `{instance="<Endpoints>"}` or `{endpoint="tcp-prometheus-servicemonitor"}`.
