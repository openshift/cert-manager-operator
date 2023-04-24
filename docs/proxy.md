# Configuring egress proxy for Cert Manager Operator

If a cluster wide egress proxy is configured on the OpenShift cluster, OLM automatically update all the operators' deployments with `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` environment variables.  
Those variables are then propagated down to the cert-manager (operand) controllers by the cert manager operator.

## Trusted Certificate Authority

### Running operator

Follow the instructions below to let Cert Manager Operator trust a custom Certificate Authority (CA). The operator's OLM subscription has to be already created.

1.  Create the configmap containing the CA bundle in `cert-manager` namespace. Run the following commands to [inject](https://docs.openshift.com/container-platform/4.12/networking/configuring-a-custom-pki.html#certificate-injection-using-operators_configuring-a-custom-pki) the CA bundle trusted by OpenShift into a configmap:

    ```bash
    oc -n cert-manager create configmap trusted-ca
    oc -n cert-manager label cm trusted-ca config.openshift.io/inject-trusted-cabundle=true
    ```

2.  Consume the created configmap in Cert Manager Operator's deployment by updating its subscription:

    ```bash
    oc -n cert-manager-operator patch subscription <subscription_name> --type='merge' -p '{"spec":{"config":{"env":[{"name":"TRUSTED_CA_CONFIGMAP_NAME","value":"trusted-ca"}]}}}'
    ```

    _Note_: Alternatively, you can also patch the `cert-manager-operator-controller-manager` deployment in the `cert-manager-operator` namespace.
    `bash
    oc set env deployment/cert-manager-operator-controller-manager TRUSTED_CA_CONFIGMAP_NAME=trusted-ca 
    `

3.  Wait for the operator deployment to finish the rollout and verify that CA bundle is added to the existing controller:

    ```bash
    oc get deployment -n cert-manager cert-manager -o=jsonpath={.spec.template.spec.'containers[0].volumeMounts'} | jq
    [
      {
        "mountPath": "/etc/pki/tls/certs/cert-manager-tls-ca-bundle.crt",
        "name": "trusted-ca",
        "subPath": "ca-bundle.crt"
      }
    ]

    oc get deployment -n cert-manager cert-manager -o=jsonpath={.spec.template.spec.volumes} | jq
    [
      {
        "configMap": {
          "defaultMode": 420,
          "name": "trusted-ca"
        },
        "name": "trusted-ca"
      }
    ]
    ```
