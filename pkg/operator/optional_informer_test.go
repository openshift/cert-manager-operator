package operator

import (
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
)

func TestOptionalConfigInformer(t *testing.T) {
	cfg, err := config.GetConfig()
	if err != nil {
		t.Fatal(err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = kubeClient

	configClient, err := configv1client.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// configClient.Discovery().
	infraInformers := configinformers.NewSharedInformerFactory(configClient, 5*time.Second)
	_ = infraInformers

	// resp, err := configClient.DiscoveryV1().Infrastructure("default").List(metav1.ListOptions{})
	// if err != nil {
	//     log.Fatalf("Error listing Infrastructures: %v", err)
	// }
	// fmt.Printf("Listing Infrastructures: %+v\n", resp.Items)

}
