package v1alpha1

import (
	"encoding/json"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/utils/ptr"
)

func TestApproverPolicyManagerScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind string
		want runtime.Object
	}{
		{kind: "ApproverPolicyManager", want: &ApproverPolicyManager{}},
		{kind: "ApproverPolicyManagerList", want: &ApproverPolicyManagerList{}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			gvk := SchemeGroupVersion.WithKind(tc.kind)
			obj, err := scheme.New(gvk)
			if err != nil {
				t.Fatal(err)
			}
			if reflect.TypeOf(obj) != reflect.TypeOf(tc.want) {
				t.Fatalf("registered type = %T, want %T", obj, tc.want)
			}
			data := []byte(`{"apiVersion":"operator.openshift.io/v1alpha1","kind":"` + tc.kind + `"}`)
			decoded, actualGVK, err := serializer.NewCodecFactory(scheme).UniversalDeserializer().Decode(data, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if *actualGVK != gvk || reflect.TypeOf(decoded) != reflect.TypeOf(tc.want) {
				t.Fatalf("decoded %T with GVK %v, want %T with %v", decoded, actualGVK, tc.want, gvk)
			}
		})
	}
}

func TestApproverPolicyManagerJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "minimal", data: `{"spec":{"approverPolicyConfig":{}}}`},
		{name: "empty optional objects", data: `{"spec":{"approverPolicyConfig":{"approveSignerNames":[],"affinity":{}},"controllerConfig":{}},"status":{}}`},
		{name: "populated", data: `{"spec":{"approverPolicyConfig":{"logLevel":5,"logFormat":"json","approveSignerNames":["issuers.cert-manager.io/*"],"resources":{"requests":{"cpu":"10m"}},"affinity":{"nodeAffinity":{}},"tolerations":[{"key":"dedicated","operator":"Exists"}],"nodeSelector":{"kubernetes.io/os":"linux"}},"controllerConfig":{"labels":{"team":"security"},"annotations":{"example.com/owner":"platform"}}},"status":{"approverPolicyImage":"example.com/approver-policy:test","conditions":[{"type":"Ready","status":"False","reason":"Progressing","message":"Pending","lastTransitionTime":"2026-01-01T00:00:00Z"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var original ApproverPolicyManager
			if err := json.Unmarshal([]byte(tc.data), &original); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(&original)
			if err != nil {
				t.Fatal(err)
			}
			var decoded ApproverPolicyManager
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if !equality.Semantic.DeepEqual(original, decoded) {
				t.Fatalf("round trip lost typed configuration: %s", encoded)
			}
			var wire struct {
				Spec struct {
					Config json.RawMessage `json:"approverPolicyConfig"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			if len(wire.Spec.Config) == 0 {
				t.Fatal("required spec.approverPolicyConfig disappeared during serialization")
			}
			if tc.name == "populated" {
				config := decoded.Spec.ApproverPolicyConfig
				if config.LogLevel != 5 || config.LogFormat != "json" || len(config.ApproveSignerNames) != 1 ||
					config.Affinity == nil || len(config.Tolerations) != 1 || config.NodeSelector["kubernetes.io/os"] != "linux" ||
					decoded.Spec.ControllerConfig.Labels["team"] != "security" || decoded.Status.ApproverPolicyImage == "" || len(decoded.Status.Conditions) != 1 {
					t.Fatalf("wire fields were not decoded into the contract: %#v", decoded)
				}
			}
		})
	}
}

func TestApproverPolicyManagerDeepCopy(t *testing.T) {
	for _, list := range []bool{false, true} {
		name := "object"
		if list {
			name = "list"
		}
		t.Run(name, func(t *testing.T) {
			original := &ApproverPolicyManager{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"original": "true"}},
				Spec: ApproverPolicyManagerSpec{
					ApproverPolicyConfig: ApproverPolicyManagerConfig{
						ApproveSignerNames: []string{"example.com/signer"},
						Resources:          corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")}},
						Affinity:           &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{{Weight: 1}}}},
						Tolerations:        []corev1.Toleration{{TolerationSeconds: ptr.To[int64](30)}},
						NodeSelector:       map[string]string{"zone": "a"},
					},
					ControllerConfig: ApproverPolicyManagerControllerConfig{Labels: map[string]string{"team": "a"}, Annotations: map[string]string{"owner": "a"}},
				},
				Status: ApproverPolicyManagerStatus{ConditionalStatus: ConditionalStatus{Conditions: []metav1.Condition{{Type: "Ready", Reason: "Progressing"}}}},
			}
			before, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			cloned := original.DeepCopy()
			if list {
				cloned = &(&ApproverPolicyManagerList{Items: []ApproverPolicyManager{*original}}).DeepCopy().Items[0]
			}
			cloned.Labels["original"] = "false"
			cloned.Spec.ApproverPolicyConfig.ApproveSignerNames[0] = "changed"
			cloned.Spec.ApproverPolicyConfig.Resources.Requests[corev1.ResourceCPU] = resource.MustParse("20m")
			cloned.Spec.ApproverPolicyConfig.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight = 2
			*cloned.Spec.ApproverPolicyConfig.Tolerations[0].TolerationSeconds = 60
			cloned.Spec.ApproverPolicyConfig.NodeSelector["zone"] = "b"
			cloned.Spec.ControllerConfig.Labels["team"] = "b"
			cloned.Spec.ControllerConfig.Annotations["owner"] = "b"
			cloned.Status.Conditions[0].Reason = "Ready"
			after, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatalf("mutating the copy changed the original:\nbefore: %s\nafter: %s", before, after)
			}
		})
	}
}
