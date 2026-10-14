module github.com/openshift/cert-manager-operator/test

go 1.26.0

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.21.0
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.13.1
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2 v2.2.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi v1.3.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources v1.2.0
	github.com/aws/aws-sdk-go-v2 v1.41.2
	github.com/aws/aws-sdk-go-v2/config v1.32.10
	github.com/aws/aws-sdk-go-v2/service/iam v1.52.1
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.7
	github.com/cert-manager/cert-manager v1.20.3
	github.com/cert-manager/trust-manager v0.25.0
	github.com/ghodss/yaml v1.0.0
	github.com/google/go-cmp v0.7.0
	github.com/google/uuid v1.6.0
	github.com/onsi/ginkgo/v2 v2.32.1
	github.com/onsi/gomega v1.43.0
	github.com/openshift/api v0.0.0-20260829233811-31af9f93e31e
	github.com/openshift/cert-manager-operator v0.0.0-00010101000000-000000000000
	github.com/openshift/client-go v0.0.0-20260810202730-ddca5e0b7146
	github.com/openshift/library-go v0.0.0-20260814122159-7b930c0d20a6
	github.com/stretchr/testify v1.12.1
	github.com/tidwall/gjson v1.18.0
	github.com/vmware-archive/yaml-patch v0.0.11
	golang.org/x/sync v0.22.0
	google.golang.org/api v0.269.0
	k8s.io/api v0.37.0
	k8s.io/apiextensions-apiserver v0.37.0
	k8s.io/apimachinery v0.37.0
	k8s.io/client-go v0.37.0
	k8s.io/utils v0.0.0-20260707023825-cf1189d6abe3
	sigs.k8s.io/controller-runtime v0.25.0
	sigs.k8s.io/yaml v1.6.0
)

require (
	cloud.google.com/go/auth v0.18.2 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.11.2 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.6.0 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.10 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.18 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.18 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.18 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.18 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.15 // indirect
	github.com/aws/smithy-go v1.24.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v1.0.0 // indirect
	github.com/go-openapi/jsonreference v1.0.0 // indirect
	github.com/go-openapi/swag v0.27.1 // indirect
	github.com/go-openapi/swag/cmdutils v0.27.1 // indirect
	github.com/go-openapi/swag/conv v0.27.1 // indirect
	github.com/go-openapi/swag/fileutils v0.27.1 // indirect
	github.com/go-openapi/swag/jsonutils v0.27.1 // indirect
	github.com/go-openapi/swag/loading v0.27.1 // indirect
	github.com/go-openapi/swag/mangling v0.27.1 // indirect
	github.com/go-openapi/swag/netutils v0.27.1 // indirect
	github.com/go-openapi/swag/pools v0.27.1 // indirect
	github.com/go-openapi/swag/stringutils v0.27.1 // indirect
	github.com/go-openapi/swag/typeutils v0.27.1 // indirect
	github.com/go-openapi/swag/yamlutils v0.27.1 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/gnostic-models v0.7.1 // indirect
	github.com/google/pprof v0.0.0-20260402051712-545e8a4df936 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.12 // indirect
	github.com/googleapis/gax-go/v2 v2.17.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/krishicks/yaml-patch v0.0.10 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/moby/spdystream v0.5.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nxadm/tail v1.4.11 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/operator-framework/operator-lib v0.19.0 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.24.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/robfig/cron v1.2.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.65.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.5.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/grpc v1.79.3 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/apiserver v0.36.4 // indirect
	k8s.io/component-base v0.37.0 // indirect
	k8s.io/component-helpers v0.36.4 // indirect
	k8s.io/controller-manager v0.36.4 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/kube-aggregator v0.36.2 // indirect
	k8s.io/kube-openapi v0.0.0-20260721132016-d427ff9ee9ad // indirect
	k8s.io/kubelet v0.36.4 // indirect
	k8s.io/kubernetes v1.36.4 // indirect
	k8s.io/streaming v0.36.4 // indirect
	sigs.k8s.io/gateway-api v1.5.0 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/kube-storage-version-migrator v0.0.6-0.20230721195810-5c8923c5ff96 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.4.2 // indirect
)

replace github.com/openshift/cert-manager-operator => ../

replace github.com/cert-manager/cert-manager => github.com/openshift/jetstack-cert-manager v1.20.3

// trust-manager v0.25.0 requires k8s.io/* v0.37.0 and controller-runtime v0.25.0.
// Pin to Kubernetes 1.36 / controller-runtime v0.24 to match OpenShift 5.0 and the operator module.
replace (
	k8s.io/api => k8s.io/api v0.36.4
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.36.4
	k8s.io/apimachinery => k8s.io/apimachinery v0.36.4
	k8s.io/apiserver => k8s.io/apiserver v0.36.4
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.36.4
	k8s.io/client-go => k8s.io/client-go v0.36.4
	k8s.io/code-generator => k8s.io/code-generator v0.36.4
	k8s.io/component-base => k8s.io/component-base v0.36.4
	k8s.io/component-helpers => k8s.io/component-helpers v0.36.4
	k8s.io/controller-manager => k8s.io/controller-manager v0.36.4
	k8s.io/kms => k8s.io/kms v0.36.4
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.36.4
	k8s.io/kubelet => k8s.io/kubelet v0.36.4
	k8s.io/streaming => k8s.io/streaming v0.36.4
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.24.1
)
