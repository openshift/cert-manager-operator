package deployment

import (
	"context"
	"fmt"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
)

type DefaultCertManagerController struct {
	operatorClient    v1helpers.OperatorClient
	controllerFactory *factory.Factory
	recorder          events.Recorder
	certManagerClient alpha1.OperatorV1alpha1Interface
}

func NewDefaultCertManagerController(operatorClient v1helpers.OperatorClient, certManagerClient alpha1.OperatorV1alpha1Interface, eventsRecorder events.Recorder) factory.Controller {
	controller := DefaultCertManagerController{
		operatorClient:    operatorClient,
		certManagerClient: certManagerClient,
		controllerFactory: factory.New().ResyncEvery(time.Minute).WithInformers(
			operatorClient.Informer(),
		),
		recorder: eventsRecorder.WithComponentSuffix("default-cert-manager-controller"),
	}

	return controller.controllerFactory.WithSync(controller.sync).ToController("DefaultCertManager", controller.recorder)
}

func (c *DefaultCertManagerController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	_, _, _, err := c.operatorClient.GetOperatorState()
	if apierrors.IsNotFound(err) {
		syncCtx.Recorder().Eventf("StatusNotFound", "Creating \"cluster\" certmanager")
		_, err = c.createDefaultCertManager(ctx)
		if err != nil {
			return fmt.Errorf("failed to create default certmanager: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get operator state: %w", err)
	}
	return nil
}

func (c *DefaultCertManagerController) createDefaultCertManager(ctx context.Context) (*v1alpha1.CertManager, error) {
	cm := &v1alpha1.CertManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: v1alpha1.CertManagerSpec{
			OperatorSpec: operatorv1.OperatorSpec{
				ManagementState: operatorv1.Managed,
			},
		},
	}
	return c.certManagerClient.CertManagers().Create(ctx, cm, metav1.CreateOptions{})
}
