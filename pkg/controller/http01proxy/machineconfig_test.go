package http01proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client"

	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

var machineConfigGR = schema.GroupResource{Group: "machineconfiguration.openshift.io", Resource: "machineconfigs"}

func ignitionConfig(t *testing.T, mc *mcfgv1.MachineConfig) map[string]interface{} {
	t.Helper()
	if len(mc.Spec.Config.Raw) == 0 {
		t.Fatal("MachineConfig spec.config is empty")
	}
	var config map[string]interface{}
	if err := json.Unmarshal(mc.Spec.Config.Raw, &config); err != nil {
		t.Fatalf("failed to unmarshal ignition config: %v", err)
	}
	return config
}

func extractNFTRules(t *testing.T, mc *mcfgv1.MachineConfig) string {
	t.Helper()
	config := ignitionConfig(t, mc)
	storage, ok := config["storage"].(map[string]interface{})
	if !ok {
		t.Fatal("storage not found in ignition config")
	}
	files, ok := storage["files"].([]interface{})
	if !ok || len(files) == 0 {
		t.Fatal("storage files not found")
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

func extractUnitContents(t *testing.T, mc *mcfgv1.MachineConfig) string {
	t.Helper()
	config := ignitionConfig(t, mc)
	systemd, ok := config["systemd"].(map[string]interface{})
	if !ok {
		t.Fatal("systemd not found in ignition config")
	}
	units, ok := systemd["units"].([]interface{})
	if !ok || len(units) == 0 {
		t.Fatal("systemd units not found")
	}
	unit := units[0].(map[string]interface{})
	return unit["contents"].(string)
}

func extractUnit(t *testing.T, mc *mcfgv1.MachineConfig) map[string]interface{} {
	t.Helper()
	config := ignitionConfig(t, mc)
	systemd := config["systemd"].(map[string]interface{})
	units := systemd["units"].([]interface{})
	return units[0].(map[string]interface{})
}

// reorderJSONKeys re-serializes a JSON object with its top-level keys in
// reverse order: same meaning, different bytes. This simulates an API server
// round-trip that reformats the stored runtime.RawExtension payload.
func reorderJSONKeys(t *testing.T, raw []byte) []byte {
	t.Helper()
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		keyJSON, err := json.Marshal(k)
		if err != nil {
			t.Fatalf("failed to marshal key: %v", err)
		}
		parts = append(parts, string(keyJSON)+":"+string(config[k]))
	}
	return []byte("{" + strings.Join(parts, ",") + "}")
}

// stubGetMachineConfig makes the fake client return the given MachineConfig
// from Get calls.
func stubGetMachineConfig(fakeClient *fakes.FakeCtrlClient, existing *mcfgv1.MachineConfig) {
	fakeClient.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		existing.DeepCopyInto(obj.(*mcfgv1.MachineConfig))
		return nil
	}
}

// vipFamilies pairs one API VIP with one ingress VIP of the same family.
func vipFamilies(t *testing.T, apiVIP, ingressVIP string) []vipFamily {
	t.Helper()
	families, err := vipFamiliesFor([]string{apiVIP}, []string{ingressVIP})
	if err != nil {
		t.Fatalf("vipFamiliesFor() error: %v", err)
	}
	return families
}

// vipInfo builds a platformInfo with one API VIP and one ingress VIP.
func vipInfo(apiVIP, ingressVIP string) *platformInfo {
	return &platformInfo{apiVIPs: []string{apiVIP}, ingressVIPs: []string{ingressVIP}}
}

func TestRenderMachineConfig(t *testing.T) {
	mc, err := renderMachineConfig(vipFamilies(t, "10.46.97.1", "10.46.97.48"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
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

	unit := extractUnit(t, mc)
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
	if !strings.Contains(unitContents, "iptables -I FORWARD 1 -p tcp -s 10.46.97.48/32 --sport 80 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT") {
		t.Error("unit should insert iptables FORWARD ACCEPT rule for replies from ingress VIP")
	}
	if !strings.Contains(unitContents, "iptables -D FORWARD -p tcp -s 10.46.97.48/32 --sport 80 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT") {
		t.Error("unit ExecStop should remove iptables FORWARD reply rule")
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
	if strings.Contains(nftRules, "ip6 ") {
		t.Errorf("IPv4-only nftables rules should not contain IPv6 rules:\n%s", nftRules)
	}
	for _, unwanted := range []string{"net.ipv6.conf.all.forwarding", "ip6tables"} {
		if strings.Contains(unitContents, unwanted) {
			t.Errorf("IPv4-only unit should not contain %q:\n%s", unwanted, unitContents)
		}
	}
}

func TestRenderMachineConfigDifferentVIP(t *testing.T) {
	mc, err := renderMachineConfig(vipFamilies(t, "192.168.1.1", "192.168.1.100"))
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
	mc, err := renderMachineConfig(vipFamilies(t, "10.0.0.254", "10.0.0.1"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	config := ignitionConfig(t, mc)
	ignition, ok := config["ignition"].(map[string]interface{})
	if !ok {
		t.Fatal("ignition not found")
	}
	version, ok := ignition["version"].(string)
	if !ok || version != "3.4.0" {
		t.Errorf("ignition version = %q, want 3.4.0", version)
	}
}

func TestRenderMachineConfigFilePermissions(t *testing.T) {
	mc, err := renderMachineConfig(vipFamilies(t, "10.0.0.254", "10.0.0.1"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	config := ignitionConfig(t, mc)
	storage := config["storage"].(map[string]interface{})
	files := storage["files"].([]interface{})
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

func TestRenderMachineConfigFingerprint(t *testing.T) {
	mc, err := renderMachineConfig(vipFamilies(t, "10.0.0.254", "10.0.0.1"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	fingerprint := mc.GetAnnotations()[machineConfigFingerprintAnnotation]
	if fingerprint == "" {
		t.Fatal("rendered MachineConfig should carry the config fingerprint annotation")
	}

	// The fingerprint must be deterministic for identical inputs.
	mcAgain, err := renderMachineConfig(vipFamilies(t, "10.0.0.254", "10.0.0.1"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}
	if got := mcAgain.GetAnnotations()[machineConfigFingerprintAnnotation]; got != fingerprint {
		t.Errorf("fingerprint not deterministic: got %q for identical inputs, want %q", got, fingerprint)
	}

	// A different input must produce a different fingerprint.
	mcOther, err := renderMachineConfig(vipFamilies(t, "10.0.0.253", "10.0.0.1"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}
	if got := mcOther.GetAnnotations()[machineConfigFingerprintAnnotation]; got == fingerprint {
		t.Error("fingerprint should change when the API VIP changes")
	}
}

// TestRenderMachineConfigIPv4Golden pins the IPv4-only render. A change here
// changes the fingerprint and rolls a new MachineConfig (and a reboot) out to
// every control plane node of existing single-stack clusters.
func TestRenderMachineConfigIPv4Golden(t *testing.T) {
	mc, err := renderMachineConfig(vipFamilies(t, "10.46.97.1", "10.46.97.48"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	wantNFT := `table inet crtmgr_http01_dnat
delete table inet crtmgr_http01_dnat
table inet crtmgr_http01_dnat {
    chain prerouting {
        type nat hook prerouting priority 0;
        ip daddr 10.46.97.1 tcp dport 80 dnat ip to 10.46.97.48:80
    }
    chain postrouting {
        type nat hook postrouting priority 100;
        ip daddr 10.46.97.48 tcp dport 80 masquerade
    }
}
`
	if got := extractNFTRules(t, mc); got != wantNFT {
		t.Errorf("nftables rules mismatch\ngot:\n%s\nwant:\n%s", got, wantNFT)
	}

	const wantFingerprint = "029c90a5348d04dd0ce8d98ae2ce52d7771a002b6037890be28e0f647e49c668"
	if got := mc.Annotations[machineConfigFingerprintAnnotation]; got != wantFingerprint {
		t.Errorf("fingerprint = %q, want %q", got, wantFingerprint)
	}
}

func TestRenderMachineConfigIPv6(t *testing.T) {
	mc, err := renderMachineConfig(vipFamilies(t, "fd00::1", "fd00::48"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	// The nftables rules are pinned by TestRenderMachineConfigIPv6Golden.
	unitContents := extractUnitContents(t, mc)
	for _, want := range []string{
		"ExecStartPre=/sbin/sysctl -w net.ipv6.conf.all.forwarding=1",
		"/usr/sbin/ip6tables -C FORWARD -p tcp -d fd00::48/128 --dport 80 -j ACCEPT",
		"/usr/sbin/ip6tables -I FORWARD 1 -p tcp -d fd00::48/128 --dport 80 -j ACCEPT",
		"/usr/sbin/ip6tables -D FORWARD -p tcp -d fd00::48/128 --dport 80 -j ACCEPT",
		"/usr/sbin/ip6tables -I FORWARD 1 -p tcp -s fd00::48/128 --sport 80 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
		"/usr/sbin/ip6tables -D FORWARD -p tcp -s fd00::48/128 --sport 80 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
	} {
		if !strings.Contains(unitContents, want) {
			t.Errorf("unit missing %q:\n%s", want, unitContents)
		}
	}
	for _, unwanted := range []string{"net.ipv4.ip_forward", "/usr/sbin/iptables "} {
		if strings.Contains(unitContents, unwanted) {
			t.Errorf("IPv6-only unit should not contain %q:\n%s", unwanted, unitContents)
		}
	}
}

func TestRenderMachineConfigDualStack(t *testing.T) {
	families, err := vipFamiliesFor([]string{"10.0.0.254", "fd00::254"}, []string{"10.0.0.1", "fd00::1"})
	if err != nil {
		t.Fatalf("vipFamiliesFor() error: %v", err)
	}
	mc, err := renderMachineConfig(families)
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	wantNFT := `table inet crtmgr_http01_dnat
delete table inet crtmgr_http01_dnat
table inet crtmgr_http01_dnat {
    chain prerouting {
        type nat hook prerouting priority 0;
        ip daddr 10.0.0.254 tcp dport 80 dnat ip to 10.0.0.1:80
        ip6 daddr fd00::254 tcp dport 80 dnat ip6 to [fd00::1]:80
    }
    chain postrouting {
        type nat hook postrouting priority 100;
        ip daddr 10.0.0.1 tcp dport 80 masquerade
        ip6 daddr fd00::1 tcp dport 80 masquerade
    }
}
`
	if got := extractNFTRules(t, mc); got != wantNFT {
		t.Errorf("nftables rules mismatch\ngot:\n%s\nwant:\n%s", got, wantNFT)
	}

	unitContents := extractUnitContents(t, mc)
	for _, want := range []string{
		"ExecStartPre=/sbin/sysctl -w net.ipv4.ip_forward=1",
		"ExecStartPre=/sbin/sysctl -w net.ipv6.conf.all.forwarding=1",
		"/usr/sbin/iptables -I FORWARD 1 -p tcp -d 10.0.0.1/32 --dport 80 -j ACCEPT",
		"/usr/sbin/ip6tables -I FORWARD 1 -p tcp -d fd00::1/128 --dport 80 -j ACCEPT",
		"/usr/sbin/iptables -D FORWARD -p tcp -d 10.0.0.1/32 --dport 80 -j ACCEPT",
		"/usr/sbin/ip6tables -D FORWARD -p tcp -d fd00::1/128 --dport 80 -j ACCEPT",
		"/usr/sbin/iptables -I FORWARD 1 -p tcp -s 10.0.0.1/32 --sport 80 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
		"/usr/sbin/ip6tables -I FORWARD 1 -p tcp -s fd00::1/128 --sport 80 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
	} {
		if !strings.Contains(unitContents, want) {
			t.Errorf("unit missing %q:\n%s", want, unitContents)
		}
	}
}

func TestRenderMachineConfigDualStackFingerprint(t *testing.T) {
	render := func(apiVIPs, ingressVIPs []string) string {
		t.Helper()
		families, err := vipFamiliesFor(apiVIPs, ingressVIPs)
		if err != nil {
			t.Fatalf("vipFamiliesFor() error: %v", err)
		}
		mc, err := renderMachineConfig(families)
		if err != nil {
			t.Fatalf("renderMachineConfig() error: %v", err)
		}
		return mc.Annotations[machineConfigFingerprintAnnotation]
	}

	v4First := render([]string{"10.0.0.254", "fd00::254"}, []string{"10.0.0.1", "fd00::1"})
	v6First := render([]string{"fd00::254", "10.0.0.254"}, []string{"fd00::1", "10.0.0.1"})
	if v4First != v6First {
		t.Errorf("fingerprint depends on VIP order: %q vs %q", v4First, v6First)
	}

	v4Only := render([]string{"10.0.0.254"}, []string{"10.0.0.1"})
	if v4Only == v4First {
		t.Error("adding the IPv6 family should change the fingerprint")
	}

	v6Only := render([]string{"fd00::254"}, []string{"fd00::1"})
	if v6Only == v4First || v6Only == v4Only {
		t.Error("IPv6-only fingerprint should differ from the dual-stack and IPv4-only ones")
	}

	if render([]string{"10.0.0.254", "fd00::253"}, []string{"10.0.0.1", "fd00::1"}) == v4First {
		t.Error("fingerprint should change when the IPv6 API VIP changes")
	}
	if render([]string{"10.0.0.254", "fd00::254"}, []string{"10.0.0.1", "fd00::2"}) == v4First {
		t.Error("fingerprint should change when the IPv6 ingress VIP changes")
	}
	if render([]string{"fd00::254"}, []string{"fd00::1"}) != v6Only {
		t.Error("IPv6-only fingerprint should be deterministic")
	}
}

func TestCreateOrApplyMachineConfigCreateDualStack(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(errors.NewNotFound(machineConfigGR, machineConfigName))
	fakeClient.CreateReturns(nil)

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), &platformInfo{
		apiVIPs:     []string{"fd00::254", "10.0.0.254"},
		ingressVIPs: []string{"fd00::1", "10.0.0.1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeClient.CreateCallCount() != 1 {
		t.Fatalf("expected 1 Create call, got %d", fakeClient.CreateCallCount())
	}
	_, obj, _ := fakeClient.CreateArgsForCall(0)
	nftRules := extractNFTRules(t, obj.(*mcfgv1.MachineConfig))
	for _, want := range []string{"dnat ip to 10.0.0.1:80", "dnat ip6 to [fd00::1]:80"} {
		if !strings.Contains(nftRules, want) {
			t.Errorf("created MachineConfig nftables rules missing %q:\n%s", want, nftRules)
		}
	}
}

func TestCreateOrApplyMachineConfigCreate(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(errors.NewNotFound(machineConfigGR, machineConfigName))
	fakeClient.CreateReturns(nil)

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), vipInfo("10.0.0.253", "10.0.0.2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeClient.CreateCallCount() != 1 {
		t.Errorf("expected 1 Create call, got %d", fakeClient.CreateCallCount())
	}

	_, created, _ := fakeClient.CreateArgsForCall(0)
	mc := created.(*mcfgv1.MachineConfig)
	if mc.GetName() != machineConfigName {
		t.Errorf("created MachineConfig name = %q, want %q", mc.GetName(), machineConfigName)
	}
	if mc.GetAnnotations()[machineConfigFingerprintAnnotation] == "" {
		t.Error("created MachineConfig should carry the config fingerprint annotation")
	}
}

func TestCreateOrApplyMachineConfigCreateError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(errors.NewNotFound(machineConfigGR, machineConfigName))
	fakeClient.CreateReturns(fmt.Errorf("forbidden"))

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), vipInfo("10.0.0.253", "10.0.0.2"))
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

	err := r.createOrApplyMachineConfig(context.Background(), vipInfo("10.0.0.253", "10.0.0.2"))
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

func TestCreateOrApplyMachineConfigUpdateWhenAnnotationMissing(t *testing.T) {
	// A pre-existing MachineConfig that predates the fingerprint scheme.
	existing := &mcfgv1.MachineConfig{}
	existing.SetName(machineConfigName)
	existing.SetResourceVersion("12345")
	existing.Spec.Config = runtime.RawExtension{Raw: []byte(`{"ignition":{"version":"old"}}`)}

	fakeClient := &fakes.FakeCtrlClient{}
	stubGetMachineConfig(fakeClient, existing)
	fakeClient.UpdateReturns(nil)

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), vipInfo("10.0.0.253", "10.0.0.2"))
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
	mc := updated.(*mcfgv1.MachineConfig)
	if mc.GetResourceVersion() != "12345" {
		t.Errorf("Update should preserve resourceVersion, got %q", mc.GetResourceVersion())
	}
	if mc.GetAnnotations()[machineConfigFingerprintAnnotation] == "" {
		t.Error("updated MachineConfig should carry the config fingerprint annotation")
	}
}

func TestCreateOrApplyMachineConfigUpdateWhenVIPsChange(t *testing.T) {
	// An existing MachineConfig rendered from different VIPs: the fingerprint
	// no longer matches, so the controller must Update.
	existing, err := renderMachineConfig(vipFamilies(t, "10.0.0.253", "10.0.0.2"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}
	existing.SetResourceVersion("77")

	fakeClient := &fakes.FakeCtrlClient{}
	stubGetMachineConfig(fakeClient, existing)
	fakeClient.UpdateReturns(nil)

	r := newTestReconciler(fakeClient)

	err = r.createOrApplyMachineConfig(context.Background(), vipInfo("10.0.0.254", "10.0.0.3"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeClient.UpdateCallCount() != 1 {
		t.Errorf("expected 1 Update call, got %d", fakeClient.UpdateCallCount())
	}

	_, updated, _ := fakeClient.UpdateArgsForCall(0)
	mc := updated.(*mcfgv1.MachineConfig)
	if mc.GetResourceVersion() != "77" {
		t.Errorf("Update should preserve resourceVersion, got %q", mc.GetResourceVersion())
	}
	if mc.GetAnnotations()[machineConfigFingerprintAnnotation] == existing.GetAnnotations()[machineConfigFingerprintAnnotation] {
		t.Error("Update should refresh the fingerprint annotation when VIPs change")
	}
}

func TestCreateOrApplyMachineConfigUpdateError(t *testing.T) {
	existing := &mcfgv1.MachineConfig{}
	existing.SetName(machineConfigName)
	existing.SetResourceVersion("12345")
	existing.Spec.Config = runtime.RawExtension{Raw: []byte(`{"ignition":{"version":"stale"}}`)}

	fakeClient := &fakes.FakeCtrlClient{}
	stubGetMachineConfig(fakeClient, existing)
	fakeClient.UpdateReturns(fmt.Errorf("conflict"))

	r := newTestReconciler(fakeClient)

	err := r.createOrApplyMachineConfig(context.Background(), vipInfo("10.0.0.253", "10.0.0.2"))
	if err == nil {
		t.Fatal("expected error when Update fails")
	}
	if !strings.Contains(err.Error(), "failed to update MachineConfig") {
		t.Errorf("error = %q, want substring 'failed to update MachineConfig'", err.Error())
	}
}

func TestCreateOrApplyMachineConfigNoOpWhenUnchanged(t *testing.T) {
	desired, err := renderMachineConfig(vipFamilies(t, "10.0.0.253", "10.0.0.2"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	// Simulate an API server round-trip that reformatted the stored JSON
	// (reordered top-level keys) without changing its meaning: the bytes
	// differ from the rendered config, but the fingerprint still matches.
	existing := desired.DeepCopy()
	existing.Spec.Config = runtime.RawExtension{Raw: reorderJSONKeys(t, desired.Spec.Config.Raw)}
	if bytes.Equal(existing.Spec.Config.Raw, desired.Spec.Config.Raw) {
		t.Fatal("test setup error: reformatted JSON should differ from rendered bytes")
	}

	fakeClient := &fakes.FakeCtrlClient{}
	stubGetMachineConfig(fakeClient, existing)

	r := newTestReconciler(fakeClient)

	err = r.createOrApplyMachineConfig(context.Background(), vipInfo("10.0.0.253", "10.0.0.2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeClient.UpdateCallCount() != 0 {
		t.Error("should not Update when the fingerprint matches, even if the stored JSON was reformatted")
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
	mc := deleted.(*mcfgv1.MachineConfig)
	if mc.GetName() != machineConfigName {
		t.Errorf("deleted object name = %q, want %q", mc.GetName(), machineConfigName)
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

// TestRenderMachineConfigIPv6Golden pins the IPv6-only render. A change here
// rolls a new MachineConfig (and a reboot) out to IPv6-only clusters.
func TestRenderMachineConfigIPv6Golden(t *testing.T) {
	mc, err := renderMachineConfig(vipFamilies(t, "fd00::1", "fd00::48"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	wantNFT := `table inet crtmgr_http01_dnat
delete table inet crtmgr_http01_dnat
table inet crtmgr_http01_dnat {
    chain prerouting {
        type nat hook prerouting priority 0;
        ip6 daddr fd00::1 tcp dport 80 dnat ip6 to [fd00::48]:80
    }
    chain postrouting {
        type nat hook postrouting priority 100;
        ip6 daddr fd00::48 tcp dport 80 masquerade
    }
}
`
	if got := extractNFTRules(t, mc); got != wantNFT {
		t.Errorf("nftables rules mismatch\ngot:\n%s\nwant:\n%s", got, wantNFT)
	}

	const wantFingerprint = "b3345862711a3dc6dfe0a5afa045e876c187242e83c97b41816e34a5efe46ecf"
	if got := mc.Annotations[machineConfigFingerprintAnnotation]; got != wantFingerprint {
		t.Errorf("fingerprint = %q, want %q", got, wantFingerprint)
	}
}

// TestRenderMachineConfigDualStackGolden pins the dual-stack render with the
// VIPs of the cluster it was validated on end-to-end. The fingerprint matches
// the MachineConfig that was rolled out and tested there.
func TestRenderMachineConfigDualStackGolden(t *testing.T) {
	families, err := vipFamiliesFor(
		[]string{"10.46.97.32", "2620:52:0:2e61::1632"},
		[]string{"10.46.97.48", "2620:52:0:2e61::1648"},
	)
	if err != nil {
		t.Fatalf("vipFamiliesFor() error: %v", err)
	}
	mc, err := renderMachineConfig(families)
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	wantNFT := `table inet crtmgr_http01_dnat
delete table inet crtmgr_http01_dnat
table inet crtmgr_http01_dnat {
    chain prerouting {
        type nat hook prerouting priority 0;
        ip daddr 10.46.97.32 tcp dport 80 dnat ip to 10.46.97.48:80
        ip6 daddr 2620:52:0:2e61::1632 tcp dport 80 dnat ip6 to [2620:52:0:2e61::1648]:80
    }
    chain postrouting {
        type nat hook postrouting priority 100;
        ip daddr 10.46.97.48 tcp dport 80 masquerade
        ip6 daddr 2620:52:0:2e61::1648 tcp dport 80 masquerade
    }
}
`
	if got := extractNFTRules(t, mc); got != wantNFT {
		t.Errorf("nftables rules mismatch\ngot:\n%s\nwant:\n%s", got, wantNFT)
	}

	const wantFingerprint = "1c0c667c84b8825bf4f1de40c6e3cc57fca51a26efd8647106d15110129956cd"
	if got := mc.Annotations[machineConfigFingerprintAnnotation]; got != wantFingerprint {
		t.Errorf("fingerprint = %q, want %q", got, wantFingerprint)
	}
}

// Each family's unit commands must come as matching pairs: ExecStart checks
// (-C) and inserts (-I) exactly the rule that ExecStop deletes (-D), or a
// stop leaves rules behind on the node.
func TestRenderMachineConfigForwardRulesPairedPerFamily(t *testing.T) {
	families, err := vipFamiliesFor([]string{"10.0.0.254", "fd00::254"}, []string{"10.0.0.1", "fd00::1"})
	if err != nil {
		t.Fatalf("vipFamiliesFor() error: %v", err)
	}
	mc, err := renderMachineConfig(families)
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}
	unitContents := extractUnitContents(t, mc)

	for _, f := range families {
		cidr := fmt.Sprintf("%s/%d", f.IngressVIP, f.HostPrefixLen)
		for _, rule := range []string{
			"-p tcp -d " + cidr + " --dport 80 -j ACCEPT",
			"-p tcp -s " + cidr + " --sport 80 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
		} {
			for _, op := range []string{" -C FORWARD ", " -I FORWARD 1 ", " -D FORWARD "} {
				want := f.IPTables + op + rule
				if n := strings.Count(unitContents, want); n != 1 {
					t.Errorf("unit should contain %q exactly once, found %d", want, n)
				}
			}
		}
		if want := "sysctl -w " + f.ForwardSysctl + "=1"; !strings.Contains(unitContents, want) {
			t.Errorf("unit missing %q", want)
		}
	}
	// Neither family's binary may be used with the other family's address.
	if strings.Contains(unitContents, "/usr/sbin/iptables -C FORWARD -p tcp -d fd00") ||
		strings.Contains(unitContents, "/usr/sbin/ip6tables -C FORWARD -p tcp -d 10.") {
		t.Errorf("iptables/ip6tables used with the wrong address family:\n%s", unitContents)
	}
	for _, wrongPrefix := range []string{"fd00::1/32", "10.0.0.1/128"} {
		if strings.Contains(unitContents, wrongPrefix) {
			t.Errorf("unit renders %q with the other family's prefix length:\n%s", wrongPrefix, unitContents)
		}
	}
}

// systemd runs ExecStart lines in order and stops at the first failure, and
// ExecStop does not run after a failed start. Loading the nftables rules must
// stay the first ExecStart so a failed load leaves no FORWARD rules behind.
func TestRenderMachineConfigNFTLoadsBeforeForwardRules(t *testing.T) {
	families, err := vipFamiliesFor([]string{"10.0.0.254", "fd00::254"}, []string{"10.0.0.1", "fd00::1"})
	if err != nil {
		t.Fatalf("vipFamiliesFor() error: %v", err)
	}
	mc, err := renderMachineConfig(families)
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}

	var execStarts []string
	for _, line := range strings.Split(extractUnitContents(t, mc), "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			execStarts = append(execStarts, line)
		}
	}
	// One nft load plus a request and a reply rule per family.
	if want := 1 + 2*len(families); len(execStarts) != want {
		t.Fatalf("got %d ExecStart lines, want %d:\n%s", len(execStarts), want, strings.Join(execStarts, "\n"))
	}
	if want := "ExecStart=/sbin/nft -f /etc/sysconfig/nftables-crtmgr-http01.conf"; execStarts[0] != want {
		t.Errorf("first ExecStart = %q, want %q", execStarts[0], want)
	}
}

func TestRenderMachineConfigIPv6Normalized(t *testing.T) {
	mc, err := renderMachineConfig(vipFamilies(t, "FD00:0000::0001", "fd00:0:0:0:0:0:0:48"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}
	normalized, err := renderMachineConfig(vipFamilies(t, "fd00::1", "fd00::48"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}
	if got, want := mc.Annotations[machineConfigFingerprintAnnotation], normalized.Annotations[machineConfigFingerprintAnnotation]; got != want {
		t.Errorf("fingerprint of non-canonical IPv6 input = %q, want canonical %q", got, want)
	}
	if nft := extractNFTRules(t, mc); !strings.Contains(nft, "dnat ip6 to [fd00::48]:80") {
		t.Errorf("IPv6 VIPs should be rendered in canonical form:\n%s", nft)
	}
}

func TestCreateOrApplyMachineConfigCreateIPv6(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(errors.NewNotFound(machineConfigGR, machineConfigName))
	fakeClient.CreateReturns(nil)

	r := newTestReconciler(fakeClient)

	if err := r.createOrApplyMachineConfig(context.Background(), vipInfo("fd00::254", "fd00::1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeClient.CreateCallCount() != 1 {
		t.Fatalf("expected 1 Create call, got %d", fakeClient.CreateCallCount())
	}
	_, obj, _ := fakeClient.CreateArgsForCall(0)
	nftRules := extractNFTRules(t, obj.(*mcfgv1.MachineConfig))
	if !strings.Contains(nftRules, "dnat ip6 to [fd00::1]:80") {
		t.Errorf("created MachineConfig nftables rules missing IPv6 DNAT:\n%s", nftRules)
	}
	if strings.Contains(nftRules, "ip daddr") {
		t.Errorf("IPv6-only MachineConfig should not contain IPv4 rules:\n%s", nftRules)
	}
}

func TestCreateOrApplyMachineConfigInvalidVIPs(t *testing.T) {
	tests := []struct {
		name    string
		info    *platformInfo
		wantErr string
	}{
		{name: "invalid IPv4 VIP", info: vipInfo("10.0.0.256", "10.0.0.1"), wantErr: "invalid API VIP"},
		{name: "invalid IPv6 VIP", info: vipInfo("fd00::254", "fd00::zz"), wantErr: "invalid ingress VIP"},
		{name: "zoned IPv6 VIP", info: vipInfo("fe80::1%br-ex", "fe80::2"), wantErr: "IPv6 zone"},
		{name: "IPv6 API with IPv4 ingress", info: vipInfo("fd00::254", "10.0.0.1"), wantErr: "share no IP family"},
		{name: "empty API VIPs", info: &platformInfo{apiVIPs: []string{}, ingressVIPs: []string{"10.0.0.1"}}, wantErr: "share no IP family"},
		{name: "empty ingress VIPs", info: &platformInfo{apiVIPs: []string{"fd00::254"}, ingressVIPs: []string{}}, wantErr: "share no IP family"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeCtrlClient{}
			r := newTestReconciler(fakeClient)

			err := r.createOrApplyMachineConfig(context.Background(), tt.info)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("createOrApplyMachineConfig() error = %v, want substring %q", err, tt.wantErr)
			}
			if fakeClient.GetCallCount() != 0 || fakeClient.CreateCallCount() != 0 || fakeClient.UpdateCallCount() != 0 {
				t.Errorf("no API calls expected on invalid VIPs, got Get=%d Create=%d Update=%d",
					fakeClient.GetCallCount(), fakeClient.CreateCallCount(), fakeClient.UpdateCallCount())
			}
		})
	}
}

// An existing MachineConfig is updated when the rendered families change and
// left alone when they do not, for every family combination.
func TestCreateOrApplyMachineConfigFamilyTransitions(t *testing.T) {
	v4 := []string{"10.0.0.254"}
	v4Ingress := []string{"10.0.0.1"}
	v6 := []string{"fd00::254"}
	v6Ingress := []string{"fd00::1"}
	dual := []string{"10.0.0.254", "fd00::254"}
	dualIngress := []string{"10.0.0.1", "fd00::1"}

	tests := []struct {
		name                         string
		existingAPI, existingIngress []string
		api, ingress                 []string
		wantUpdate                   bool
	}{
		{name: "IPv6 unchanged", existingAPI: v6, existingIngress: v6Ingress, api: v6, ingress: v6Ingress},
		{name: "dual-stack unchanged", existingAPI: dual, existingIngress: dualIngress, api: dual, ingress: dualIngress},
		{
			name:        "dual-stack VIP order swapped",
			existingAPI: dual, existingIngress: dualIngress,
			api: []string{"fd00::254", "10.0.0.254"}, ingress: []string{"fd00::1", "10.0.0.1"},
		},
		{
			name:        "IPv6 written in non-canonical form",
			existingAPI: v6, existingIngress: v6Ingress,
			api: []string{"FD00:0::254"}, ingress: []string{"fd00:0:0::1"},
		},
		{name: "IPv6 API VIP changes", existingAPI: v6, existingIngress: v6Ingress, api: []string{"fd00::253"}, ingress: v6Ingress, wantUpdate: true},
		{name: "IPv6 ingress VIP changes", existingAPI: v6, existingIngress: v6Ingress, api: v6, ingress: []string{"fd00::2"}, wantUpdate: true},
		{name: "IPv4 to dual-stack", existingAPI: v4, existingIngress: v4Ingress, api: dual, ingress: dualIngress, wantUpdate: true},
		{name: "dual-stack to IPv4", existingAPI: dual, existingIngress: dualIngress, api: v4, ingress: v4Ingress, wantUpdate: true},
		{name: "IPv6 to dual-stack", existingAPI: v6, existingIngress: v6Ingress, api: dual, ingress: dualIngress, wantUpdate: true},
		{name: "dual-stack to IPv6", existingAPI: dual, existingIngress: dualIngress, api: v6, ingress: v6Ingress, wantUpdate: true},
		{name: "IPv4 to IPv6", existingAPI: v4, existingIngress: v4Ingress, api: v6, ingress: v6Ingress, wantUpdate: true},
		{
			name:        "IPv6 dropped from ingress VIPs",
			existingAPI: dual, existingIngress: dualIngress,
			api: dual, ingress: v4Ingress, wantUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			families, err := vipFamiliesFor(tt.existingAPI, tt.existingIngress)
			if err != nil {
				t.Fatalf("vipFamiliesFor() error: %v", err)
			}
			existing, err := renderMachineConfig(families)
			if err != nil {
				t.Fatalf("renderMachineConfig() error: %v", err)
			}
			existing.SetResourceVersion("42")

			fakeClient := &fakes.FakeCtrlClient{}
			stubGetMachineConfig(fakeClient, existing)
			fakeClient.UpdateReturns(nil)

			r := newTestReconciler(fakeClient)

			err = r.createOrApplyMachineConfig(context.Background(), &platformInfo{apiVIPs: tt.api, ingressVIPs: tt.ingress})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fakeClient.CreateCallCount() != 0 {
				t.Errorf("should not Create when MachineConfig exists, got %d", fakeClient.CreateCallCount())
			}
			if !tt.wantUpdate {
				if fakeClient.UpdateCallCount() != 0 {
					t.Errorf("expected no Update, got %d", fakeClient.UpdateCallCount())
				}
				return
			}
			if fakeClient.UpdateCallCount() != 1 {
				t.Fatalf("expected 1 Update, got %d", fakeClient.UpdateCallCount())
			}
			_, obj, _ := fakeClient.UpdateArgsForCall(0)
			updated := obj.(*mcfgv1.MachineConfig)
			if updated.GetResourceVersion() != "42" {
				t.Errorf("Update should preserve resourceVersion, got %q", updated.GetResourceVersion())
			}
			wantFamilies, err := vipFamiliesFor(tt.api, tt.ingress)
			if err != nil {
				t.Fatalf("vipFamiliesFor() error: %v", err)
			}
			want, err := renderMachineConfig(wantFamilies)
			if err != nil {
				t.Fatalf("renderMachineConfig() error: %v", err)
			}
			if got, wantFP := updated.Annotations[machineConfigFingerprintAnnotation], want.Annotations[machineConfigFingerprintAnnotation]; got != wantFP {
				t.Errorf("updated fingerprint = %q, want %q", got, wantFP)
			}
			if !bytes.Equal(updated.Spec.Config.Raw, want.Spec.Config.Raw) {
				t.Error("updated spec.config should match the newly rendered config")
			}
		})
	}
}
