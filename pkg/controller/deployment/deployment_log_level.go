package deployment

import (
	appsv1 "k8s.io/api/apps/v1"

	v1 "github.com/openshift/api/operator/v1"
)

var logLevels = map[v1.LogLevel]string{
	v1.Normal:   "--v=2",
	v1.Debug:    "--v=4",
	v1.Trace:    "--v=6",
	v1.TraceAll: "--v=8",
}

// withLogLevel sets the values of verbosity --v arg using
// logLevel specified in spec
func withLogLevel(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	verbosityArg := logLevels[operatorSpec.LogLevel]
	if verbosityArg == "" {
		return nil
	}

	deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
		deployment.Spec.Template.Spec.Containers[0].Args,
		[]string{verbosityArg},
	)
	return nil
}
