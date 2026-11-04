package http01proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"text/template"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"

	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

var (
	nftRulesTmpl = template.Must(template.New("nft").Parse(
		string(assets.MustAsset(nftRulesAssetName)),
	))
	machineConfigTmpl = template.Must(template.New("mc").Parse(
		string(assets.MustAsset(machineConfigAssetName)),
	))
)

type nftRulesRenderData struct {
	Families []vipFamily
}

type machineConfigRenderData struct {
	Name           string
	NFTRulesBase64 string
	Families       []vipFamily
}

func renderMachineConfig(families []vipFamily) (*mcfgv1.MachineConfig, error) {
	var nftBuf bytes.Buffer
	if err := nftRulesTmpl.Execute(&nftBuf, nftRulesRenderData{Families: families}); err != nil {
		return nil, fmt.Errorf("failed to render nftables rules: %w", err)
	}

	mcData := machineConfigRenderData{
		Name:           machineConfigName,
		NFTRulesBase64: base64.StdEncoding.EncodeToString(nftBuf.Bytes()),
		Families:       families,
	}

	var mcBuf bytes.Buffer
	if err := machineConfigTmpl.Execute(&mcBuf, mcData); err != nil {
		return nil, fmt.Errorf("failed to render MachineConfig template: %w", err)
	}

	mc := &mcfgv1.MachineConfig{}
	if err := yaml.NewYAMLOrJSONDecoder(&mcBuf, mcBuf.Len()).Decode(mc); err != nil {
		return nil, fmt.Errorf("failed to decode rendered MachineConfig: %w", err)
	}

	if err := setMachineConfigFingerprint(mc); err != nil {
		return nil, fmt.Errorf("failed to fingerprint rendered MachineConfig: %w", err)
	}

	return mc, nil
}

// setMachineConfigFingerprint annotates the MachineConfig with a SHA-256 hash
// of its rendered spec. Reconciliation compares this annotation instead of the
// spec itself: spec.config round-trips through the API server as raw JSON
// bytes (runtime.RawExtension), which can be reformatted without a semantic
// change, so comparing the spec can report changes where there are none.
// Hashing the full spec rather than just the ignition config also picks up
// changes to other spec fields if the template ever sets them.
func setMachineConfigFingerprint(mc *mcfgv1.MachineConfig) error {
	specJSON, err := json.Marshal(mc.Spec)
	if err != nil {
		return fmt.Errorf("failed to marshal MachineConfig spec: %w", err)
	}
	fingerprint := sha256.Sum256(specJSON)
	metav1.SetMetaDataAnnotation(&mc.ObjectMeta, machineConfigFingerprintAnnotation, hex.EncodeToString(fingerprint[:]))
	return nil
}

func (r *Reconciler) createOrApplyMachineConfig(ctx context.Context, info *platformInfo) error {
	families, err := vipFamiliesFor(info.apiVIPs, info.ingressVIPs)
	if err != nil {
		return fmt.Errorf("failed to determine VIPs for MachineConfig: %w", err)
	}

	desired, err := renderMachineConfig(families)
	if err != nil {
		return fmt.Errorf("failed to render DNAT MachineConfig: %w", err)
	}

	existing := &mcfgv1.MachineConfig{}
	err = r.Get(ctx, types.NamespacedName{Name: machineConfigName}, existing)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get MachineConfig %q: %w", machineConfigName, err)
		}
		r.log.V(2).Info("creating MachineConfig", "name", machineConfigName)
		if err := r.Create(ctx, desired); err != nil {
			return fmt.Errorf("failed to create MachineConfig %q: %w", machineConfigName, err)
		}
		return nil
	}

	if existing.Annotations[machineConfigFingerprintAnnotation] == desired.Annotations[machineConfigFingerprintAnnotation] {
		r.log.V(4).Info("MachineConfig unchanged, skipping update", "name", machineConfigName)
		return nil
	}

	desired.SetResourceVersion(existing.GetResourceVersion())
	r.log.V(2).Info("updating MachineConfig", "name", machineConfigName)
	if err := r.Update(ctx, desired); err != nil {
		return fmt.Errorf("failed to update MachineConfig %q: %w", machineConfigName, err)
	}
	return nil
}

func (r *Reconciler) deleteMachineConfig(ctx context.Context) error {
	mc := &mcfgv1.MachineConfig{}
	mc.SetName(machineConfigName)
	if err := r.Delete(ctx, mc); err != nil {
		if errors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("failed to delete MachineConfig %q: %w", machineConfigName, err)
	}
	r.log.V(2).Info("deleted MachineConfig", "name", machineConfigName)
	return nil
}
