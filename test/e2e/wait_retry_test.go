//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"net/http"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsRetriableAPIError(t *testing.T) {
	timeout504 := &apierrors.StatusError{ErrStatus: metav1.Status{
		Status:  metav1.StatusFailure,
		Message: "the server was unable to return a response in the time allotted, but may still be processing the request (get deployments.apps cert-manager-operator-controller-manager)",
		Reason:  metav1.StatusReasonTimeout,
		Code:    http.StatusGatewayTimeout,
	}}
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "missing")
	forbidden := apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "x", fmt.Errorf("denied"))

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "kube-apiserver 504 Timeout", err: timeout504, want: true},
		{name: "server timeout", err: apierrors.NewServerTimeout(schema.GroupResource{Resource: "deployments"}, "get", 1), want: true},
		{name: "too many requests", err: apierrors.NewTooManyRequests("slow down", 1), want: true},
		{name: "service unavailable", err: apierrors.NewServiceUnavailable("unavailable"), want: true},
		{name: "internal error", err: apierrors.NewInternalError(fmt.Errorf("boom")), want: true},
		{name: "connection reset", err: fmt.Errorf("read: connection reset by peer"), want: true},
		{name: "not found is not retriable here", err: notFound, want: false},
		{name: "forbidden", err: forbidden, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetriableAPIError(tt.err); got != tt.want {
				t.Fatalf("isRetriableAPIError() = %v, want %v (err=%v)", got, tt.want, tt.err)
			}
		})
	}
}

func TestDeploymentArgsContain(t *testing.T) {
	args := []string{
		"--acme-http01-solver-resource-limits-cpu=150m",
		"--acme-http01-solver-resource-request-memory=100Mi",
	}
	withArgs := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Args: append([]string{"--v=2"}, args...),
					}},
				},
			},
		},
	}
	if !deploymentArgsContain(withArgs, args) {
		t.Fatal("expected deployment args to contain the solver resource flags")
	}
	if deploymentArgsContain(withArgs, []string{"--acme-http01-solver-resource-limits-memory=200Mi"}) {
		t.Fatal("expected a missing solver resource flag to fail the check")
	}
	if deploymentArgsContain(nil, args) || deploymentArgsContain(&appsv1.Deployment{}, args) {
		t.Fatal("expected a deployment without containers to fail the check")
	}
}
