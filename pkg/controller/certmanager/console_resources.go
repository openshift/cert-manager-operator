package certmanager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	sigyaml "sigs.k8s.io/yaml"

	consolev1 "github.com/openshift/api/console/v1"
	consoleclient "github.com/openshift/client-go/console/clientset/versioned"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

const (
	consoleResourcesControllerName = operatorName + "-console-resources"
)

var (
	ConsoleYAMLSampleGVR = schema.GroupVersionResource{
		Group:    "console.openshift.io",
		Version:  "v1",
		Resource: "consoleyamlsamples",
	}

	ConsoleQuickStartGVR = schema.GroupVersionResource{
		Group:    "console.openshift.io",
		Version:  "v1",
		Resource: "consolequickstarts",
	}

	yamlSampleAssetFiles = []string{
		"console/cert-manager-acme-issuer-sample.yaml",
		"console/cert-manager-certificate-sample.yaml",
		"console/cert-manager-issuer-sample.yaml",
	}

	quickStartAssetFiles = []string{
		"console/cert-manager-example-quickstart.yaml",
	}
)

// +kubebuilder:rbac:groups=console.openshift.io,resources=consoleyamlsamples;consolequickstarts,verbs=get;create;update

type consoleResourcesController struct {
	consoleClient consoleclient.Interface
	eventRecorder events.Recorder
	yamlSamples   []*consolev1.ConsoleYAMLSample
	quickStarts   []*consolev1.ConsoleQuickStart
}

func loadAssets[T any](files []string) []*T {
	result := make([]*T, 0, len(files))
	for _, file := range files {
		data, err := assets.Asset(file)
		if err != nil {
			panic(fmt.Sprintf("failed to read console asset %s: %v", file, err))
		}
		var obj T
		if err := sigyaml.UnmarshalStrict(data, &obj); err != nil {
			panic(fmt.Sprintf("failed to decode console asset %s: %v", file, err))
		}
		result = append(result, &obj)
	}
	return result
}

func NewConsoleResourcesController(
	operatorClient v1helpers.OperatorClient,
	consoleClient consoleclient.Interface,
	eventRecorder events.Recorder,
) factory.Controller {
	c := &consoleResourcesController{
		consoleClient: consoleClient,
		eventRecorder: eventRecorder.WithComponentSuffix("console-resources"),
		yamlSamples:   loadAssets[consolev1.ConsoleYAMLSample](yamlSampleAssetFiles),
		quickStarts:   loadAssets[consolev1.ConsoleQuickStart](quickStartAssetFiles),
	}

	return factory.New().
		WithInformers(operatorClient.Informer()).
		ResyncEvery(10*time.Minute).
		WithSync(c.sync).
		ToController(consoleResourcesControllerName, c.eventRecorder)
}

func (c *consoleResourcesController) sync(ctx context.Context, _ factory.SyncContext) error {
	var errs []error

	yamlClient := c.consoleClient.ConsoleV1().ConsoleYAMLSamples()
	for _, desired := range c.yamlSamples {
		if err := applyConsoleResource(ctx, c.eventRecorder, "ConsoleYAMLSample", desired.Name, desired,
			yamlClient.Get, yamlClient.Create, yamlClient.Update,
			func(a, b *consolev1.ConsoleYAMLSample) bool { return equality.Semantic.DeepEqual(a.Spec, b.Spec) },
			func(existing, desired *consolev1.ConsoleYAMLSample) *consolev1.ConsoleYAMLSample {
				u := existing.DeepCopy(); u.Spec = desired.Spec; return u
			},
		); err != nil {
			errs = append(errs, fmt.Errorf("failed to apply ConsoleYAMLSample/%s: %w", desired.Name, err))
		}
	}

	qsClient := c.consoleClient.ConsoleV1().ConsoleQuickStarts()
	for _, desired := range c.quickStarts {
		if err := applyConsoleResource(ctx, c.eventRecorder, "ConsoleQuickStart", desired.Name, desired,
			qsClient.Get, qsClient.Create, qsClient.Update,
			func(a, b *consolev1.ConsoleQuickStart) bool { return equality.Semantic.DeepEqual(a.Spec, b.Spec) },
			func(existing, desired *consolev1.ConsoleQuickStart) *consolev1.ConsoleQuickStart {
				u := existing.DeepCopy(); u.Spec = desired.Spec; return u
			},
		); err != nil {
			errs = append(errs, fmt.Errorf("failed to apply ConsoleQuickStart/%s: %w", desired.Name, err))
		}
	}

	return errors.Join(errs...)
}

func applyConsoleResource[T any](
	ctx context.Context,
	recorder events.Recorder,
	kind, name string,
	desired *T,
	get func(context.Context, string, metav1.GetOptions) (*T, error),
	create func(context.Context, *T, metav1.CreateOptions) (*T, error),
	update func(context.Context, *T, metav1.UpdateOptions) (*T, error),
	specsEqual func(existing, desired *T) bool,
	withDesiredSpec func(existing, desired *T) *T,
) error {
	existing, err := get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err = create(ctx, desired, metav1.CreateOptions{}); err != nil {
			return err
		}
		recorder.Eventf(kind+"Created", "Created %s %q", kind, name)
		return nil
	}
	if err != nil {
		return err
	}
	if specsEqual(existing, desired) {
		return nil
	}
	if _, err = update(ctx, withDesiredSpec(existing, desired), metav1.UpdateOptions{}); err != nil {
		return err
	}
	recorder.Eventf(kind+"Updated", "Updated %s %q", kind, name)
	return nil
}
