package operator

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// ManagedController implements a controller that can be setup and run
// with a controller-runtime based manager.
type ManagedController interface {
	// SetupWithManager assigns the controller a manager
	SetupWithManager(mgr ctrl.Manager) error
}
