package http01proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"text/template"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
	APIVIP     string
	IngressVIP string
}

type machineConfigRenderData struct {
	Name           string
	NFTRulesBase64 string
	IngressVIP     string
}

func renderMachineConfig(apiVIP, ingressVIP string) (*mcfgv1.MachineConfig, error) {
	var nftBuf bytes.Buffer
	if err := nftRulesTmpl.Execute(&nftBuf, nftRulesRenderData{APIVIP: apiVIP, IngressVIP: ingressVIP}); err != nil {
		return nil, fmt.Errorf("failed to render nftables rules: %w", err)
	}

	mcData := machineConfigRenderData{
		Name:           machineConfigName,
		NFTRulesBase64: base64.StdEncoding.EncodeToString(nftBuf.Bytes()),
		IngressVIP:     ingressVIP,
	}

	var mcBuf bytes.Buffer
	if err := machineConfigTmpl.Execute(&mcBuf, mcData); err != nil {
		return nil, fmt.Errorf("failed to render MachineConfig template: %w", err)
	}

	mc := &mcfgv1.MachineConfig{}
	if err := yaml.NewYAMLOrJSONDecoder(&mcBuf, mcBuf.Len()).Decode(mc); err != nil {
		return nil, fmt.Errorf("failed to decode rendered MachineConfig: %w", err)
	}

	return mc, nil
}

func (r *Reconciler) createOrApplyMachineConfig(ctx context.Context, info *platformInfo) error {
	if len(info.apiVIPs) == 0 {
		return fmt.Errorf("no API VIPs available for MachineConfig")
	}
	if len(info.ingressVIPs) == 0 {
		return fmt.Errorf("no ingress VIPs available for MachineConfig")
	}

	desired, err := renderMachineConfig(info.apiVIPs[0], info.ingressVIPs[0])
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

	if reflect.DeepEqual(desired.Spec, existing.Spec) {
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
