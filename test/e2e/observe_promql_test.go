//go:build e2e
// +build e2e

package e2e

import "testing"

func TestPrometheusUpEqualsOne(t *testing.T) {
	upOne := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","job":"cert-manager","namespace":"cert-manager","instance":"10.0.0.1:9402"},"value":[1,"1"]}]}}`
	upZero := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","job":"cert-manager","namespace":"cert-manager","instance":"10.0.0.1:9402"},"value":[1,"0"]}]}}`
	empty := `{"status":"success","data":{"resultType":"vector","result":[]}}`
	mixed := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up","job":"cert-manager","namespace":"cert-manager","instance":"10.0.0.1:9402"},"value":[1,"1"]},{"metric":{"__name__":"up","job":"cert-manager","namespace":"cert-manager","instance":"10.0.0.2:9402"},"value":[1,"0"]}]}}`

	if ok, _ := prometheusUpEqualsOne(upOne, "cert-manager"); !ok {
		t.Fatal("expected up=1 to pass")
	}
	if ok, reason := prometheusUpEqualsOne(upZero, "cert-manager"); ok {
		t.Fatalf("expected up=0 to fail, got pass")
	} else if reason == "" {
		t.Fatal("expected a failure reason for up=0")
	}
	if ok, _ := prometheusUpEqualsOne(empty, "cert-manager"); ok {
		t.Fatal("expected empty result to fail")
	}
	if ok, _ := prometheusUpEqualsOne(mixed, "cert-manager"); ok {
		t.Fatal("expected mixed up values to fail")
	}
}

func TestPrometheusMetricPresent(t *testing.T) {
	present := `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"certmanager_clock_time_seconds","job":"cert-manager","namespace":"cert-manager"},"value":[1,"1747897156"]}]}}`
	empty := `{"status":"success","data":{"resultType":"vector","result":[]}}`

	if ok, _ := prometheusMetricPresent(present, "certmanager_clock_time_seconds", "cert-manager"); !ok {
		t.Fatal("expected operand metric to pass")
	}
	if ok, _ := prometheusMetricPresent(empty, "certmanager_clock_time_seconds", "cert-manager"); ok {
		t.Fatal("expected empty operand metric to fail")
	}
}
