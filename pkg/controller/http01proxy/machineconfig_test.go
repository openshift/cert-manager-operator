package http01proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

var machineConfigGR = schema.GroupResource{Group: "machineconfiguration.openshift.io", Resource: "machineconfigs"}

func extractNFTRules(t *testing.T, mc *unstructured.Unstructured) string {
	t.Helper()
	files, found, err := unstructured.NestedSlice(mc.Object, "spec", "config", "storage", "files")
	if err != nil || !found || len(files) == 0 {
		t.Fatalf("storage files not found: found=%v err=%v", found, err)
	}
	file := files[0].(map[string]interface{})
	contents := file["contents"].(map[string]interface{})
	source := contents["source"].(string)
	b64Data := strings.TrimPrefix(source, "data:text/plain;charset=utf-8;base64,")
	decoded, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		t.Fatalf("failed to decode base64 nftables rules: %v", err)
	}
	return string(decoded)
}

func extractUnitContents(t *testing.T, mc *unstructured.Unstructured) string {
	t.Helper()
	units, found, err := unstructured.NestedSlice(mc.Object, "spec", "config", "systemd", "units")
	if err != nil || !found || len(units) == 0 {
		t.Fatalf("systemd units not found: found=%v err=%v", found, err)
	}
	unit := units[0].(map[string]interface{})
	return unit["contents"].(string)
}

func TestRenderMachineConfig(t *testing.T) {
	mc, err := renderMachineConfig("10.46.97.1", "10.46.97.48")
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	if mc.GetKind() != "MachineConfig" {
		t.Errorf("kind = %q, want MachineConfig", mc.GetKind())
	}
	if mc.GetName() != machineConfigName {
		t.Errorf("name = %q, want %q", mc.GetName(), machineConfigName)
	}

	labels := mc.GetLabels()
	if labels["machineconfiguration.openshift.io/role"] != "master" {
		t.Errorf("role label = %q, want master", labels["machineconfiguration.openshift.io/role"])
	}

	nftRules := extractNFTRules(t, mc)

	if !strings.Contains(nftRules, "10.46.97.48") {
		t.Error("nftables rules should contain the ingress VIP")
	}
	if !strings.Contains(nftRules, "ip daddr 10.46.97.1") {
		t.Error("nftables DNAT rule should match API VIP as destination")
	}
	if !strings.Contains(nftRules, "dnat ip to 10.46.97.48:80") {
		t.Error("nftables rules should contain DNAT rule with 'dnat ip to' for inet table")
	}
	if !strings.Contains(nftRules, "masquerade") {
		t.Error("nftables rules should contain masquerade rule")
	}
	if !strings.Contains(nftRules, "crtmgr_http01_dnat") {
		t.Error("nftables rules should reference the table name")
	}
	if !strings.Contains(nftRules, "table inet crtmgr_http01_dnat") {
		t.Error("nftables rules should use inet (dual-stack) family")
	}
	if !strings.Contains(nftRules, "hook prerouting") {
		t.Error("nftables rules should have prerouting chain")
	}
	if !strings.Contains(nftRules, "hook postrouting") {
		t.Error("nftables rules should have postrouting chain")
	}

	units, _, _ := unstructured.NestedSlice(mc.Object, "spec", "config", "systemd", "units")
	unit := units[0].(map[string]interface{})
	unitContents := unit["contents"].(string)

	if !strings.Contains(unitContents, "nft -f /etc/sysconfig/nftables-crtmgr-http01.conf") {
		t.Error("unit should load nftables rules from config file")
	}
	if !strings.Contains(unitContents, "sysctl -w net.ipv4.ip_forward=1") {
		t.Error("unit should enable ip_forward via sysctl")
	}
	if !strings.Contains(unitContents, "iptables -C FORWARD") {
		t.Error("unit should check for existing iptables FORWARD rule before inserting")
	}
	if !strings.Contains(unitContents, "iptables -I FORWARD 1 -p tcp -d 10.46.97.48/32 --dport 80 -j ACCEPT") {
		t.Error("unit should insert iptables FORWARD ACCEPT rule for ingress VIP")
	}
	if !strings.Contains(unitContents, "ExecStop=/sbin/nft") {
		t.Error("unit ExecStop should clean up nftables table")
	}
	if !strings.Contains(unitContents, "iptables -D FORWARD -p tcp -d 10.46.97.48/32 --dport 80 -j ACCEPT") {
		t.Error("unit ExecStop should remove iptables FORWARD rule")
	}
	if !strings.Contains(unitContents, "Type=oneshot") {
		t.Error("unit should be Type=oneshot")
	}
	if !strings.Contains(unitContents, "RemainAfterExit=yes") {
		t.Error("unit should have RemainAfterExit=yes")
	}
	if unit["name"] != "crtmgr-http01-dnat.service" {
		t.Errorf("unit name = %q, want crtmgr-http01-dnat.service", unit["name"])
	}
	if unit["enabled"] != true {
		t.Errorf("unit enabled = %v, want true", unit["enabled"])
	}
}

func TestRenderMachineConfigDifferentVIP(t *testing.T) {
	mc, err := renderMachineConfig("192.168.1.1", "192.168.1.100")
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	nftRules := extractNFTRules(t, mc)
	if !strings.Contains(nftRules, "192.168.1.100") {
		t.Error("nftables rules should contain the provided VIP")
	}
	if !strings.Contains(nftRules, "dnat ip to 192.168.1.100:80") {
		t.Error("nftables DNAT rule should use the provided VIP")
	}

	unitContents := extractUnitContents(t, mc)
	if !strings.Contains(unitContents, "192.168.1.100") {
		t.Error("systemd unit iptables rules should reference the provided VIP")
	}
}

func TestRenderMachineConfigIgnitionVersion(t *testing.T) {
	mc, err := renderMachineConfig("10.0.0.254", "10.0.0.1")
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	version, found, err := unstructured.NestedString(mc.Object, "spec", "config", "ignition", "version")
	if err != nil || !found {
		t.Fatalf("ignition version not found: found=%v err=%v", found, err)
	}
	if version != "3.4.0" {
		t.Errorf("ignition version = %q, want 3.4.0", version)
	}
}

func TestRenderMachineConfigFilePermissions(t *testing.T) {
	mc, err := renderMachineConfig("10.0.0.254", "10.0.0.1")
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	files, _, _ := unstructured.NestedSlice(mc.Object, "spec", "config", "storage", "files")
	file := files[0].(map[string]interface{})

	path, ok := file["path"].(string)
	if !ok || path != "/etc/sysconfig/nftables-crtmgr-http01.conf" {
		t.Errorf("file path = %q, want /etc/sysconfig/nftables-crtmgr-http01.conf", path)
	}

	mode, ok := file["mode"].(float64)
	if !ok || mode != 384 { // 0600 octal
		t.Errorf("file mode = %v, want 384 (0600)", mode)
	}
}

func TestCreateOrApplyMachineConfigNoVIPs(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), &platformInfo{
		apiVIPs:     []string{},
		ingressVIPs: []string{"10.0.0.1"},
	})
	if err == nil {
		t.Fatal("expected error for empty apiVIPs")
	}
	if !strings.Contains(err.Error(), "no API VIPs") {
		t.Errorf("error = %q, want substring 'no API VIPs'", err.Error())
	}

	err = r.createOrApplyMachineConfig(context.Background(), &platformInfo{
		apiVIPs:     []string{"10.0.0.254"},
		ingressVIPs: []string{},
	})
	if err == nil {
		t.Fatal("expected error for empty ingressVIPs")
	}
	if !strings.Contains(err.Error(), "no ingress VIPs") {
		t.Errorf("error = %q, want substring 'no ingress VIPs'", err.Error())
	}
	if fakeClient.GetCallCount() != 0 {
		t.Errorf("expected 0 Get calls, got %d", fakeClient.GetCallCount())
	}
}

func TestCreateOrApplyMachineConfigCreate(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(errors.NewNotFound(machineConfigGR, machineConfigName))
	fakeClient.CreateReturns(nil)

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), &platformInfo{
		apiVIPs:     []string{"10.0.0.253"},
		ingressVIPs: []string{"10.0.0.2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeClient.CreateCallCount() != 1 {
		t.Errorf("expected 1 Create call, got %d", fakeClient.CreateCallCount())
	}

	_, created, _ := fakeClient.CreateArgsForCall(0)
	u := created.(*unstructured.Unstructured)
	if u.GetName() != machineConfigName {
		t.Errorf("created MachineConfig name = %q, want %q", u.GetName(), machineConfigName)
	}
}

func TestCreateOrApplyMachineConfigCreateError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(errors.NewNotFound(machineConfigGR, machineConfigName))
	fakeClient.CreateReturns(fmt.Errorf("forbidden"))

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), &platformInfo{
		apiVIPs:     []string{"10.0.0.253"},
		ingressVIPs: []string{"10.0.0.2"},
	})
	if err == nil {
		t.Fatal("expected error when Create fails")
	}
	if !strings.Contains(err.Error(), "failed to create MachineConfig") {
		t.Errorf("error = %q, want substring 'failed to create MachineConfig'", err.Error())
	}
}

func TestCreateOrApplyMachineConfigGetError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(fmt.Errorf("connection refused"))

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), &platformInfo{
		apiVIPs:     []string{"10.0.0.253"},
		ingressVIPs: []string{"10.0.0.2"},
	})
	if err == nil {
		t.Fatal("expected error when Get fails with non-NotFound")
	}
	if !strings.Contains(err.Error(), "failed to get MachineConfig") {
		t.Errorf("error = %q, want substring 'failed to get MachineConfig'", err.Error())
	}
	if fakeClient.CreateCallCount() != 0 {
		t.Error("should not attempt Create when Get fails")
	}
}

func TestCreateOrApplyMachineConfigUpdateWhenSpecDiffers(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		u := obj.(*unstructured.Unstructured)
		u.SetGroupVersionKind(machineConfigGVK)
		u.SetName(machineConfigName)
		u.SetResourceVersion("12345")
		// Existing spec has a different VIP
		unstructured.SetNestedField(u.Object, "old-ignition-data", "spec", "config", "ignition", "version")
		return nil
	}
	fakeClient.UpdateReturns(nil)

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), &platformInfo{
		apiVIPs:     []string{"10.0.0.253"},
		ingressVIPs: []string{"10.0.0.2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeClient.UpdateCallCount() != 1 {
		t.Errorf("expected 1 Update call, got %d", fakeClient.UpdateCallCount())
	}
	if fakeClient.CreateCallCount() != 0 {
		t.Error("should not Create when MachineConfig already exists")
	}

	_, updated, _ := fakeClient.UpdateArgsForCall(0)
	u := updated.(*unstructured.Unstructured)
	if u.GetResourceVersion() != "12345" {
		t.Errorf("Update should preserve resourceVersion, got %q", u.GetResourceVersion())
	}
}

func TestCreateOrApplyMachineConfigUpdateError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		u := obj.(*unstructured.Unstructured)
		u.SetGroupVersionKind(machineConfigGVK)
		u.SetName(machineConfigName)
		u.SetResourceVersion("12345")
		unstructured.SetNestedField(u.Object, "stale", "spec", "config", "ignition", "version")
		return nil
	}
	fakeClient.UpdateReturns(fmt.Errorf("conflict"))

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), &platformInfo{
		apiVIPs:     []string{"10.0.0.253"},
		ingressVIPs: []string{"10.0.0.2"},
	})
	if err == nil {
		t.Fatal("expected error when Update fails")
	}
	if !strings.Contains(err.Error(), "failed to update MachineConfig") {
		t.Errorf("error = %q, want substring 'failed to update MachineConfig'", err.Error())
	}
}

func TestCreateOrApplyMachineConfigNoOpWhenUnchanged(t *testing.T) {
	desired, err := renderMachineConfig("10.0.0.253", "10.0.0.2")
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}
	desiredSpec, _, _ := unstructured.NestedMap(desired.Object, "spec")

	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		u := obj.(*unstructured.Unstructured)
		u.SetGroupVersionKind(machineConfigGVK)
		u.SetName(machineConfigName)
		u.SetResourceVersion("99")
		unstructured.SetNestedField(u.Object, desiredSpec, "spec")
		return nil
	}

	r := newTestReconciler(fakeClient)

	err = r.createOrApplyMachineConfig(context.Background(), &platformInfo{
		apiVIPs:     []string{"10.0.0.253"},
		ingressVIPs: []string{"10.0.0.2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeClient.UpdateCallCount() != 0 {
		t.Error("should not Update when spec is unchanged")
	}
	if fakeClient.CreateCallCount() != 0 {
		t.Error("should not Create when MachineConfig already exists")
	}
}

func TestDeleteMachineConfigSuccess(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.DeleteReturns(nil)

	r := newTestReconciler(fakeClient)

	err := r.deleteMachineConfig(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeClient.DeleteCallCount() != 1 {
		t.Errorf("expected 1 Delete call, got %d", fakeClient.DeleteCallCount())
	}

	_, deleted, _ := fakeClient.DeleteArgsForCall(0)
	u := deleted.(*unstructured.Unstructured)
	if u.GetName() != machineConfigName {
		t.Errorf("deleted object name = %q, want %q", u.GetName(), machineConfigName)
	}
	if u.GetObjectKind().GroupVersionKind() != machineConfigGVK {
		t.Errorf("deleted object GVK = %v, want %v", u.GetObjectKind().GroupVersionKind(), machineConfigGVK)
	}
}

func TestDeleteMachineConfigNotFound(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.DeleteReturns(errors.NewNotFound(machineConfigGR, machineConfigName))

	r := newTestReconciler(fakeClient)

	err := r.deleteMachineConfig(context.Background())
	if err != nil {
		t.Fatalf("deleteMachineConfig should succeed when MachineConfig is NotFound, got: %v", err)
	}
}

func TestDeleteMachineConfigError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.DeleteReturns(fmt.Errorf("permission denied"))

	r := newTestReconciler(fakeClient)

	err := r.deleteMachineConfig(context.Background())
	if err == nil {
		t.Fatal("expected error when Delete fails")
	}
	if !strings.Contains(err.Error(), "failed to delete MachineConfig") {
		t.Errorf("error = %q, want substring 'failed to delete MachineConfig'", err.Error())
	}
}
