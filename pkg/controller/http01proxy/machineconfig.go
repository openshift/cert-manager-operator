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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const nftRulesTemplate = `table inet crtmgr_http01_dnat
delete table inet crtmgr_http01_dnat
table inet crtmgr_http01_dnat {
    chain prerouting {
        type nat hook prerouting priority 0;
        ip daddr {{ .APIVIP }} tcp dport 80 dnat ip to {{ .IngressVIP }}:80
    }
    chain postrouting {
        type nat hook postrouting priority 100;
        ip daddr {{ .IngressVIP }} tcp dport 80 masquerade
    }
}
`

const machineConfigTemplate = `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: master
  name: {{ .Name }}
spec:
  config:
    ignition:
      version: 3.4.0
    storage:
      files:
        - contents:
            source: data:text/plain;charset=utf-8;base64,{{ .NFTRulesBase64 }}
          mode: 384
          overwrite: true
          path: /etc/sysconfig/nftables-crtmgr-http01.conf
    systemd:
      units:
        - contents: |
            [Unit]
            Description=cert-manager HTTP01 DNAT nftables rules
            Wants=network-pre.target
            Before=network-pre.target
            [Service]
            Type=oneshot
            ProtectSystem=full
            ProtectHome=true
            ExecStartPre=/sbin/sysctl -w net.ipv4.ip_forward=1
            ExecStart=/sbin/nft -f /etc/sysconfig/nftables-crtmgr-http01.conf
            ExecStart=/bin/bash -c '/usr/sbin/iptables -C FORWARD -p tcp -d {{ .IngressVIP }}/32 --dport 80 -j ACCEPT 2>/dev/null || /usr/sbin/iptables -I FORWARD 1 -p tcp -d {{ .IngressVIP }}/32 --dport 80 -j ACCEPT'
            ExecReload=/sbin/nft -f /etc/sysconfig/nftables-crtmgr-http01.conf
            ExecStop=/sbin/nft 'add table inet crtmgr_http01_dnat; delete table inet crtmgr_http01_dnat'
            ExecStop=/bin/bash -c '/usr/sbin/iptables -D FORWARD -p tcp -d {{ .IngressVIP }}/32 --dport 80 -j ACCEPT 2>/dev/null; true'
            RemainAfterExit=yes
            [Install]
            WantedBy=multi-user.target
          enabled: true
          name: crtmgr-http01-dnat.service
`

var (
	nftRulesTmpl      = template.Must(template.New("nft").Parse(nftRulesTemplate))
	machineConfigTmpl = template.Must(template.New("mc").Parse(machineConfigTemplate))
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

func renderMachineConfig(apiVIP, ingressVIP string) (*unstructured.Unstructured, error) {
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

	obj := &unstructured.Unstructured{}
	if err := yaml.NewYAMLOrJSONDecoder(&mcBuf, mcBuf.Len()).Decode(&obj.Object); err != nil {
		return nil, fmt.Errorf("failed to decode rendered MachineConfig: %w", err)
	}

	return obj, nil
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

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(machineConfigGVK)
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

	desiredSpec, _, _ := unstructured.NestedMap(desired.Object, "spec")
	existingSpec, _, _ := unstructured.NestedMap(existing.Object, "spec")
	if reflect.DeepEqual(desiredSpec, existingSpec) {
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
	mc := &unstructured.Unstructured{}
	mc.SetGroupVersionKind(machineConfigGVK)
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
