package deployment

import (
	appsv1 "k8s.io/api/apps/v1"

	operatorv1 "github.com/openshift/api/operator/v1"
)

var logLevels = map[operatorv1.LogLevel]string{
	operatorv1.Normal:   "--v=2",
	operatorv1.Debug:    "--v=4",
	operatorv1.Trace:    "--v=6",
	operatorv1.TraceAll: "--v=8",
}

// withLogLevel sets the values of verbosity --v arg using
// logLevel specified in spec.
func withLogLevel(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
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
