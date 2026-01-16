//go:build e2e
// +build e2e

package library

import (
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cert-manager-operator/pkg/testutil"
)

// Scheme is re-exported from pkg/testutil for backward compatibility
// with e2e tests that import from test/library.
var Scheme *runtime.Scheme = testutil.Scheme
