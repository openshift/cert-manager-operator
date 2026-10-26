package approverpolicy

import "time"

const (
	// ControllerName identifies the controller in logs and events.
	ControllerName = "cert-manager-approver-policy-controller"

	// RequestEnqueueLabelValue identifies managed resources using common.ManagedResourceLabelKey.
	RequestEnqueueLabelValue = "cert-manager-approver-policy"

	approverPolicyManagerObjectName = "cluster"
	finalizer                       = "approverpolicymanager.operator.openshift.io/" + ControllerName
	defaultRequeueTime              = 30 * time.Second
)
