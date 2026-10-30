package common

const (
	// ManagedResourceLabelKey is the common label key used by all operand controllers
	// to identify resources they manage. Each controller uses a different value
	// to distinguish its resources.
	ManagedResourceLabelKey = "app"

	// OperatorNamespace is the namespace where cert-manager-operator runs.
	OperatorNamespace = "cert-manager-operator"

	// TrustedCABundleConfigMapName is the ConfigMap in the operator namespace
	// where CNO injects the cluster's trusted CA bundle.
	TrustedCABundleConfigMapName = "cert-manager-operator-trusted-ca-bundle"

	// OperatorTrustedCABundleAppLabelValue is the app label on the CNO-injected trusted CA
	// bundle ConfigMap so it can participate in the shared label-filtered ConfigMap cache.
	OperatorTrustedCABundleAppLabelValue = "cert-manager-operator"

	// TrustedCABundleKey is the key in the CNO-injected ConfigMap that contains the CA bundle PEM.
	TrustedCABundleKey = "ca-bundle.crt"
)
