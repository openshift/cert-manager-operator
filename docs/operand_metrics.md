## Monitoring cert-manager Metrics with OpenShift Monitoring

cert-manager exposes metrics in the format expected by [Prometheus Operator](https://github.com/prometheus-operator/prometheus-operator) for all three of its core components: controller, cainjector, and webhook.

You can configure OpenShift Monitoring to collect metrics from cert-manager operands by enabling the built-in user workload monitoring stack. This allows you to monitor user-defined projects in addition to the default platform monitoring.

### Enable User Workload Monitoring

Cluster administrators can enable monitoring for user-defined projects by setting the `enableUserWorkload: true` field in the cluster monitoring ConfigMap object. For more details, Please look at the detailed documentation to [Configuring user workload monitoring](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/monitoring/configuring-user-workload-monitoring).

1. Create or edit the ConfigMap `cluster-monitoring-config` in namespace `openshift-monitoring`.

```
$ oc apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: cluster-monitoring-config
  namespace: openshift-monitoring
data:
  config.yaml: |
    enableUserWorkload: true
EOF
```

2. Wait and check that the monitoring components for user workloads are up and running in the `openshift-user-workload-monitoring` namespace.

```
$ oc -n openshift-user-workload-monitoring get pod
NAME                                   READY   STATUS    RESTARTS   AGE
prometheus-operator-6cb6bd9588-dtzxq   2/2     Running   0          50s
prometheus-user-workload-0             6/6     Running   0          48s
prometheus-user-workload-1             6/6     Running   0          48s
thanos-ruler-user-workload-0           4/4     Running   0          42s
thanos-ruler-user-workload-1           4/4     Running   0          42s
```

You should see pods like `prometheus-operator`, `prometheus-user-workload`, and `thanos-ruler-user-workload` in a Running status.

### Configure Metric Scraping for cert-manager

cert-manager operands (controller, webhook, and cainjector) serve Prometheus metrics on port 9402 at `/metrics` over HTTPS. HTTPS is always enabled, including the default cluster TLS security profile. Plain HTTP on that port returns HTTP 400, and Prometheus marks the target down.

Each operand certificate is signed by the CA stored in Secret `cert-manager-metrics-ca`, key `ca.crt`, in the operand namespace. The operands create that Secret when they start. The certificate lists only that Service's DNS names:

- controller (`cert-manager`): `cert-manager`, `cert-manager.cert-manager`, `cert-manager.cert-manager.svc`
- webhook (`cert-manager-webhook`): `cert-manager-webhook`, `cert-manager-webhook.cert-manager`, `cert-manager-webhook.cert-manager.svc`
- cainjector (`cert-manager-cainjector`): `cert-manager-cainjector`, `cert-manager-cainjector.cert-manager`, `cert-manager-cainjector.cert-manager.svc`

`tlsConfig.serverName` is one value for every Service an endpoint selects, so one ServiceMonitor cannot verify all three targets. Use `<service>.<namespace>.svc`. The cluster DNS name `<service>.<namespace>.svc.cluster.local` is not on the certificate.

The operator sets `prometheus.io/scheme: https` on the operand pods. Prometheus Operator does not read that annotation when a ServiceMonitor selects the Service. An existing ServiceMonitor keeps its previous scheme until you change the ServiceMonitor itself.

1. Check the cert-manager services in the `cert-manager` namespace.

```
$ oc -n cert-manager get service
NAME                      TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)            AGE
cert-manager              ClusterIP   172.30.199.12   <none>        9402/TCP           54s
cert-manager-cainjector   ClusterIP   172.30.148.41   <none>        9402/TCP           63s
cert-manager-webhook      ClusterIP   172.30.100.46   <none>        443/TCP,9402/TCP   62s
```

2. Confirm the metrics CA Secret exists.

```
$ oc -n cert-manager get secret cert-manager-metrics-ca
```

3. Apply one ServiceMonitor per operand. Each scrape uses HTTPS, trusts `ca.crt` from `cert-manager-metrics-ca`, and sets `serverName` to that Service's certificate identity. Re-applying this manifest replaces an older ServiceMonitor named `cert-manager` that selected all three Services over HTTP.

```
$ oc apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    app: cert-manager
    app.kubernetes.io/instance: cert-manager
    app.kubernetes.io/name: cert-manager
  name: cert-manager
  namespace: cert-manager
spec:
  endpoints:
    - honorLabels: false
      interval: 60s
      path: /metrics
      scheme: https
      scrapeTimeout: 30s
      targetPort: 9402
      tlsConfig:
        ca:
          secret:
            key: ca.crt
            name: cert-manager-metrics-ca
        serverName: cert-manager.cert-manager.svc
  selector:
    matchLabels:
      app.kubernetes.io/name: cert-manager
      app.kubernetes.io/instance: cert-manager
      app.kubernetes.io/component: controller
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    app: cert-manager
    app.kubernetes.io/instance: cert-manager
    app.kubernetes.io/name: webhook
  name: cert-manager-webhook
  namespace: cert-manager
spec:
  endpoints:
    - honorLabels: false
      interval: 60s
      path: /metrics
      scheme: https
      scrapeTimeout: 30s
      targetPort: 9402
      tlsConfig:
        ca:
          secret:
            key: ca.crt
            name: cert-manager-metrics-ca
        serverName: cert-manager-webhook.cert-manager.svc
  selector:
    matchLabels:
      app.kubernetes.io/name: webhook
      app.kubernetes.io/instance: cert-manager
      app.kubernetes.io/component: webhook
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    app: cert-manager
    app.kubernetes.io/instance: cert-manager
    app.kubernetes.io/name: cainjector
  name: cert-manager-cainjector
  namespace: cert-manager
spec:
  endpoints:
    - honorLabels: false
      interval: 60s
      path: /metrics
      scheme: https
      scrapeTimeout: 30s
      targetPort: 9402
      tlsConfig:
        ca:
          secret:
            key: ca.crt
            name: cert-manager-metrics-ca
        serverName: cert-manager-cainjector.cert-manager.svc
  selector:
    matchLabels:
      app.kubernetes.io/name: cainjector
      app.kubernetes.io/instance: cert-manager
      app.kubernetes.io/component: cainjector
EOF
```

If the operand namespace is not `cert-manager`, set each `serverName` to `<service>.<namespace>.svc` and create the ServiceMonitors in that namespace. The Secret reference is resolved in the ServiceMonitor namespace.

Once these ServiceMonitors are in place and user workload monitoring is enabled, the Prometheus instance for user workloads will start collecting metrics from the cert-manager operands. The scraped metrics will be labeled with `job="cert-manager"`, `job="cert-manager-cainjector"`, or `job="cert-manager-webhook"` respectively.

You can select and view these Prometheus Targets via the OpenShift web console, by navigating to the "Observe" -> "Targets" page.

### Query Metrics

As a cluster administrator or as a user with view permissions for all projects, You can access these metrics using the command line or via the OpenShift web console. For more details, Please look at the detailed documentation to [Accessing metrics](https://docs.redhat.com/en/documentation/openshift_container_platform/4.20/html/monitoring/accessing-metrics).

1. Retrieve a bearer token. You can use the following command to get a token for a specific service account.
```
$ TOKEN=$(oc create token prometheus-k8s -n openshift-monitoring)
```

Alternatively, if you have cluster-admin access or view permissions for all projects, you might be able to use `$(oc whoami -t)` to get your own user token.

2. Get the OpenShift API route for Thanos Querier.

```
$ URL=$(oc get route thanos-querier -n openshift-monitoring -o=jsonpath='{.status.ingress[0].host}')
```

3. Query the metrics using `curl`, authenticating with the bearer token. The query uses the `/api/v1/query endpoint`. The output will be in JSON format, using `| jq` for pretty JSON formatting.

```
$ curl -s -k -H "Authorization: Bearer $TOKEN" https://$URL/api/v1/query --data-urlencode 'query={job="cert-manager"}' | jq
```

Example output:

```
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {
        "metric": {
          "__name__": "certmanager_clock_time_seconds",
          "container": "cert-manager-controller",
          "endpoint": "9402",
          "instance": "10.131.0.65:9402",
          "job": "cert-manager",
          "namespace": "cert-manager",
          "pod": "cert-manager-b687bdddc-sv4xt",
          "prometheus": "openshift-user-workload-monitoring/user-workload",
          "service": "cert-manager"
        },
        "value": [
          1747897178.158,
          "1747897156"
        ]
      },
      ...
      {
        "metric": {
          "__name__": "up",
          "container": "cert-manager-controller",
          "endpoint": "9402",
          "instance": "10.131.0.65:9402",
          "job": "cert-manager",
          "namespace": "cert-manager",
          "pod": "cert-manager-b687bdddc-sv4xt",
          "prometheus": "openshift-user-workload-monitoring/user-workload",
          "service": "cert-manager"
        },
        "value": [
          1747897178.158,
          "1"
        ]
      }
    ]
  }
}
```

In OpenShift web console, you can also view these metrics by navigating to the "Observe" -> "Metrics" page, and filter the metrics of each operands with `{job="<JobLabel>"}`, `{instance="<Endpoints>"}` or other advanced query expressions.
