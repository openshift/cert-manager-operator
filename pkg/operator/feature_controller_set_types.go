package operator

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

type ManagedController interface {
	// SetupWithManager assigns the controller a manager
	SetupWithManager(mgr ctrl.Manager) error
}
