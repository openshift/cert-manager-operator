package certmanager

import (
	"context"
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/clock"

	consolev1 "github.com/openshift/api/console/v1"
	consolefake "github.com/openshift/client-go/console/clientset/versioned/fake"
	"github.com/openshift/library-go/pkg/operator/events"
)

func newTestController(t *testing.T, consoleClient *consolefake.Clientset) *consoleResourcesController {
	t.Helper()
	return &consoleResourcesController{
		consoleClient: consoleClient,
		eventRecorder: events.NewInMemoryRecorder("test", clock.RealClock{}),
		yamlSamples:   loadAssets[consolev1.ConsoleYAMLSample](yamlSampleAssetFiles),
		quickStarts:   loadAssets[consolev1.ConsoleQuickStart](quickStartAssetFiles),
	}
}

func TestSyncCreatesResources(t *testing.T) {
	consoleClient := consolefake.NewClientset()

	c := newTestController(t, consoleClient)

	err := c.sync(context.Background(), nil)
	if err != nil {
		t.Fatalf("sync() returned error: %v", err)
	}

	samples, err := consoleClient.ConsoleV1().ConsoleYAMLSamples().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list ConsoleYAMLSamples: %v", err)
	}
	if len(samples.Items) != len(yamlSampleAssetFiles) {
		t.Errorf("expected %d ConsoleYAMLSamples, got %d", len(yamlSampleAssetFiles), len(samples.Items))
	}

	quickStarts, err := consoleClient.ConsoleV1().ConsoleQuickStarts().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list ConsoleQuickStarts: %v", err)
	}
	if len(quickStarts.Items) != len(quickStartAssetFiles) {
		t.Errorf("expected %d ConsoleQuickStarts, got %d", len(quickStartAssetFiles), len(quickStarts.Items))
	}
}

func TestSyncAttemptsAllAssetsOnError(t *testing.T) {
	consoleClient := consolefake.NewClientset()

	consoleClient.PrependReactor("get", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated error")
	})

	c := newTestController(t, consoleClient)
	totalAssets := len(c.yamlSamples) + len(c.quickStarts)

	err := c.sync(context.Background(), nil)
	if err == nil {
		t.Fatal("sync() should return error when all assets fail")
	}

	getActions := 0
	for _, action := range consoleClient.Actions() {
		if action.GetVerb() == "get" {
			getActions++
		}
	}

	if getActions < totalAssets {
		t.Errorf("expected at least %d get actions (one per asset), got %d; sync may be aborting early",
			totalAssets, getActions)
	}
}

func TestSyncNoAssetsNoError(t *testing.T) {
	consoleClient := consolefake.NewClientset()

	c := &consoleResourcesController{
		consoleClient: consoleClient,
		eventRecorder: events.NewInMemoryRecorder("test", clock.RealClock{}),
	}

	if err := c.sync(context.Background(), nil); err != nil {
		t.Fatalf("sync() with no assets should succeed, got: %v", err)
	}

	if len(consoleClient.Actions()) != 0 {
		t.Errorf("expected no console client actions with empty assets, got %d", len(consoleClient.Actions()))
	}
}

func TestSyncIdempotent(t *testing.T) {
	consoleClient := consolefake.NewClientset()

	c := newTestController(t, consoleClient)

	if err := c.sync(context.Background(), nil); err != nil {
		t.Fatalf("first sync() failed: %v", err)
	}

	if err := c.sync(context.Background(), nil); err != nil {
		t.Fatalf("second sync() failed: %v", err)
	}

	samples, _ := consoleClient.ConsoleV1().ConsoleYAMLSamples().List(context.Background(), metav1.ListOptions{})
	if len(samples.Items) != len(yamlSampleAssetFiles) {
		t.Errorf("expected %d ConsoleYAMLSamples after second sync, got %d", len(yamlSampleAssetFiles), len(samples.Items))
	}
}

func TestSyncReturnsAggregatedErrors(t *testing.T) {
	consoleClient := consolefake.NewClientset()

	consoleClient.PrependReactor("get", "*", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated error for %s", action.GetResource().Resource)
	})

	c := newTestController(t, consoleClient)

	err := c.sync(context.Background(), nil)
	if err == nil {
		t.Fatal("sync() should return error when all assets fail")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "ConsoleYAMLSample") {
		t.Errorf("aggregated error should mention ConsoleYAMLSample, got: %v", err)
	}
	if !strings.Contains(errStr, "ConsoleQuickStart") {
		t.Errorf("aggregated error should mention ConsoleQuickStart, got: %v", err)
	}
}

func TestSyncUpdatesChangedSpec(t *testing.T) {
	consoleClient := consolefake.NewClientset()
	c := newTestController(t, consoleClient)

	if err := c.sync(context.Background(), nil); err != nil {
		t.Fatalf("initial sync() failed: %v", err)
	}

	sample, err := consoleClient.ConsoleV1().ConsoleYAMLSamples().Get(context.Background(), "cert-manager-acme-issuer-sample", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ConsoleYAMLSample: %v", err)
	}
	sample.Spec.Description = "modified"
	if _, err := consoleClient.ConsoleV1().ConsoleYAMLSamples().Update(context.Background(), sample, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to mutate ConsoleYAMLSample: %v", err)
	}

	qs, err := consoleClient.ConsoleV1().ConsoleQuickStarts().Get(context.Background(), "cert-manager-example", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get ConsoleQuickStart: %v", err)
	}
	qs.Spec.DisplayName = "modified"
	if _, err := consoleClient.ConsoleV1().ConsoleQuickStarts().Update(context.Background(), qs, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to mutate ConsoleQuickStart: %v", err)
	}

	if err := c.sync(context.Background(), nil); err != nil {
		t.Fatalf("re-sync after mutation failed: %v", err)
	}

	updated, _ := consoleClient.ConsoleV1().ConsoleYAMLSamples().Get(context.Background(), "cert-manager-acme-issuer-sample", metav1.GetOptions{})
	if updated.Spec.Description == "modified" {
		t.Error("ConsoleYAMLSample spec should have been reverted by sync, but mutation persisted")
	}

	updatedQS, _ := consoleClient.ConsoleV1().ConsoleQuickStarts().Get(context.Background(), "cert-manager-example", metav1.GetOptions{})
	if updatedQS.Spec.DisplayName == "modified" {
		t.Error("ConsoleQuickStart spec should have been reverted by sync, but mutation persisted")
	}
}

func TestSyncPartialFailure(t *testing.T) {
	consoleClient := consolefake.NewClientset()
	c := newTestController(t, consoleClient)

	consoleClient.PrependReactor("get", "consolequickstarts", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated quickstart error")
	})

	err := c.sync(context.Background(), nil)
	if err == nil {
		t.Fatal("sync() should return error when quickstarts fail")
	}
	if !strings.Contains(err.Error(), "ConsoleQuickStart") {
		t.Errorf("error should mention ConsoleQuickStart, got: %v", err)
	}

	samples, listErr := consoleClient.ConsoleV1().ConsoleYAMLSamples().List(context.Background(), metav1.ListOptions{})
	if listErr != nil {
		t.Fatalf("failed to list ConsoleYAMLSamples: %v", listErr)
	}
	if len(samples.Items) != len(yamlSampleAssetFiles) {
		t.Errorf("expected %d ConsoleYAMLSamples to be created despite quickstart failure, got %d", len(yamlSampleAssetFiles), len(samples.Items))
	}
}

func TestSyncCreateError(t *testing.T) {
	consoleClient := consolefake.NewClientset()

	consoleClient.PrependReactor("create", "consoleyamlsamples", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated create error")
	})

	c := newTestController(t, consoleClient)

	err := c.sync(context.Background(), nil)
	if err == nil {
		t.Fatal("sync() should return error when create fails")
	}
	if !strings.Contains(err.Error(), "ConsoleYAMLSample") {
		t.Errorf("error should mention ConsoleYAMLSample, got: %v", err)
	}
}

func TestSyncUpdateError(t *testing.T) {
	consoleClient := consolefake.NewClientset()
	c := newTestController(t, consoleClient)

	if err := c.sync(context.Background(), nil); err != nil {
		t.Fatalf("initial sync() failed: %v", err)
	}

	sample, _ := consoleClient.ConsoleV1().ConsoleYAMLSamples().Get(context.Background(), "cert-manager-acme-issuer-sample", metav1.GetOptions{})
	sample.Spec.Description = "modified"
	consoleClient.ConsoleV1().ConsoleYAMLSamples().Update(context.Background(), sample, metav1.UpdateOptions{})

	consoleClient.PrependReactor("update", "consoleyamlsamples", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated update error")
	})

	err := c.sync(context.Background(), nil)
	if err == nil {
		t.Fatal("sync() should return error when update fails")
	}
	if !strings.Contains(err.Error(), "ConsoleYAMLSample") {
		t.Errorf("error should mention ConsoleYAMLSample, got: %v", err)
	}
}

func TestApplyConsoleResourceCreatesWhenNotFound(t *testing.T) {
	consoleClient := consolefake.NewClientset()
	recorder := events.NewInMemoryRecorder("test", clock.RealClock{})

	desired := &consolev1.ConsoleYAMLSample{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sample"},
		Spec:       consolev1.ConsoleYAMLSampleSpec{Title: "Test"},
	}

	client := consoleClient.ConsoleV1().ConsoleYAMLSamples()
	err := applyConsoleResource(context.Background(), recorder, "ConsoleYAMLSample", desired.Name, desired,
		client.Get, client.Create, client.Update,
		func(a, b *consolev1.ConsoleYAMLSample) bool { return a.Spec == b.Spec },
		func(existing, desired *consolev1.ConsoleYAMLSample) *consolev1.ConsoleYAMLSample {
			u := existing.DeepCopy(); u.Spec = desired.Spec; return u
		},
	)
	if err != nil {
		t.Fatalf("applyConsoleResource() returned error: %v", err)
	}

	got, err := client.Get(context.Background(), "test-sample", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("resource was not created: %v", err)
	}
	if got.Spec.Title != "Test" {
		t.Errorf("spec.title = %q, want %q", got.Spec.Title, "Test")
	}

	evts := recorder.Events()
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0].Reason != "ConsoleYAMLSampleCreated" {
		t.Errorf("event reason = %q, want %q", evts[0].Reason, "ConsoleYAMLSampleCreated")
	}
}

func TestApplyConsoleResourceUpdatesWhenSpecDiffers(t *testing.T) {
	consoleClient := consolefake.NewClientset()
	recorder := events.NewInMemoryRecorder("test", clock.RealClock{})

	existing := &consolev1.ConsoleQuickStart{
		ObjectMeta: metav1.ObjectMeta{Name: "test-qs"},
		Spec:       consolev1.ConsoleQuickStartSpec{DisplayName: "Old", DurationMinutes: 1, Description: "d", Introduction: "i", Tasks: []consolev1.ConsoleQuickStartTask{{Title: "t", Description: "d"}}},
	}
	client := consoleClient.ConsoleV1().ConsoleQuickStarts()
	if _, err := client.Create(context.Background(), existing, metav1.CreateOptions{}); err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	desired := existing.DeepCopy()
	desired.Spec.DisplayName = "New"

	err := applyConsoleResource(context.Background(), recorder, "ConsoleQuickStart", desired.Name, desired,
		client.Get, client.Create, client.Update,
		func(a, b *consolev1.ConsoleQuickStart) bool { return a.Spec.DisplayName == b.Spec.DisplayName },
		func(existing, desired *consolev1.ConsoleQuickStart) *consolev1.ConsoleQuickStart {
			u := existing.DeepCopy(); u.Spec = desired.Spec; return u
		},
	)
	if err != nil {
		t.Fatalf("applyConsoleResource() returned error: %v", err)
	}

	got, _ := client.Get(context.Background(), "test-qs", metav1.GetOptions{})
	if got.Spec.DisplayName != "New" {
		t.Errorf("spec was not updated: displayName = %q, want %q", got.Spec.DisplayName, "New")
	}

	evts := recorder.Events()
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0].Reason != "ConsoleQuickStartUpdated" {
		t.Errorf("event reason = %q, want %q", evts[0].Reason, "ConsoleQuickStartUpdated")
	}
}

func TestApplyConsoleResourceNoOpWhenSpecsEqual(t *testing.T) {
	consoleClient := consolefake.NewClientset()
	recorder := events.NewInMemoryRecorder("test", clock.RealClock{})

	existing := &consolev1.ConsoleYAMLSample{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sample"},
		Spec:       consolev1.ConsoleYAMLSampleSpec{Title: "Same"},
	}
	client := consoleClient.ConsoleV1().ConsoleYAMLSamples()
	if _, err := client.Create(context.Background(), existing, metav1.CreateOptions{}); err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	desired := existing.DeepCopy()

	err := applyConsoleResource(context.Background(), recorder, "ConsoleYAMLSample", desired.Name, desired,
		client.Get, client.Create, client.Update,
		func(a, b *consolev1.ConsoleYAMLSample) bool { return a.Spec.Title == b.Spec.Title },
		func(existing, desired *consolev1.ConsoleYAMLSample) *consolev1.ConsoleYAMLSample {
			u := existing.DeepCopy(); u.Spec = desired.Spec; return u
		},
	)
	if err != nil {
		t.Fatalf("applyConsoleResource() returned error: %v", err)
	}

	if len(recorder.Events()) != 0 {
		t.Errorf("expected 0 events for no-op, got %d", len(recorder.Events()))
	}
}

func TestApplyConsoleResourceGetError(t *testing.T) {
	consoleClient := consolefake.NewClientset()
	recorder := events.NewInMemoryRecorder("test", clock.RealClock{})

	consoleClient.PrependReactor("get", "consoleyamlsamples", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated get error")
	})

	desired := &consolev1.ConsoleYAMLSample{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sample"},
	}
	client := consoleClient.ConsoleV1().ConsoleYAMLSamples()

	err := applyConsoleResource(context.Background(), recorder, "ConsoleYAMLSample", desired.Name, desired,
		client.Get, client.Create, client.Update,
		func(a, b *consolev1.ConsoleYAMLSample) bool { return true },
		func(existing, desired *consolev1.ConsoleYAMLSample) *consolev1.ConsoleYAMLSample {
			return existing.DeepCopy()
		},
	)
	if err == nil {
		t.Fatal("expected error from get failure")
	}
	if !strings.Contains(err.Error(), "simulated get error") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestApplyConsoleResourceCreateError(t *testing.T) {
	consoleClient := consolefake.NewClientset()
	recorder := events.NewInMemoryRecorder("test", clock.RealClock{})

	consoleClient.PrependReactor("create", "consoleyamlsamples", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated create error")
	})

	desired := &consolev1.ConsoleYAMLSample{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sample"},
	}
	client := consoleClient.ConsoleV1().ConsoleYAMLSamples()

	err := applyConsoleResource(context.Background(), recorder, "ConsoleYAMLSample", desired.Name, desired,
		client.Get, client.Create, client.Update,
		func(a, b *consolev1.ConsoleYAMLSample) bool { return true },
		func(existing, desired *consolev1.ConsoleYAMLSample) *consolev1.ConsoleYAMLSample {
			return existing.DeepCopy()
		},
	)
	if err == nil {
		t.Fatal("expected error from create failure")
	}
}

func TestApplyConsoleResourceUpdateError(t *testing.T) {
	consoleClient := consolefake.NewClientset()
	recorder := events.NewInMemoryRecorder("test", clock.RealClock{})

	existing := &consolev1.ConsoleYAMLSample{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sample"},
		Spec:       consolev1.ConsoleYAMLSampleSpec{Title: "Old"},
	}
	client := consoleClient.ConsoleV1().ConsoleYAMLSamples()
	if _, err := client.Create(context.Background(), existing, metav1.CreateOptions{}); err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	consoleClient.PrependReactor("update", "consoleyamlsamples", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("simulated update error")
	})

	desired := existing.DeepCopy()
	desired.Spec.Title = "New"

	err := applyConsoleResource(context.Background(), recorder, "ConsoleYAMLSample", desired.Name, desired,
		client.Get, client.Create, client.Update,
		func(a, b *consolev1.ConsoleYAMLSample) bool { return a.Spec.Title == b.Spec.Title },
		func(existing, desired *consolev1.ConsoleYAMLSample) *consolev1.ConsoleYAMLSample {
			u := existing.DeepCopy(); u.Spec = desired.Spec; return u
		},
	)
	if err == nil {
		t.Fatal("expected error from update failure")
	}
}

func TestConsoleAssetContent(t *testing.T) {
	yamlSamples := loadAssets[consolev1.ConsoleYAMLSample](yamlSampleAssetFiles)
	expectedNames := []string{
		"cert-manager-acme-issuer-sample",
		"cert-manager-certificate-sample",
		"cert-manager-issuer-sample",
	}
	for i, sample := range yamlSamples {
		if sample.Name != expectedNames[i] {
			t.Errorf("yamlSample[%d] name = %q, want %q", i, sample.Name, expectedNames[i])
		}
	}

	quickStarts := loadAssets[consolev1.ConsoleQuickStart](quickStartAssetFiles)
	if len(quickStarts) != 1 || quickStarts[0].Name != "cert-manager-example" {
		t.Errorf("quickStart name = %q, want %q", quickStarts[0].Name, "cert-manager-example")
	}
}
