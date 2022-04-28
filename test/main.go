// Copyright 2020 The Operator-SDK Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"testing"

	"github.com/openshift/cert-manager-operator/test/e2e"
	lib "github.com/openshift/cert-manager-operator/test/library"
	scapiv1alpha3 "github.com/operator-framework/api/pkg/apis/scorecard/v1alpha3"
	apimanifests "github.com/operator-framework/api/pkg/manifests"

	"log"

	"k8s.io/klog/v2"
)

func main() {

	// Disable klog for this test to avoid printing kubernetes logs to stdout
	klog.InitFlags(nil)
	podBundleRoot := flag.String("bundle", "/bundle", "Bundle root")
	flag.Set("logtostderr", "false")
	flag.Set("alsologtostderr", "false")
	flag.Parse()

	entrypoints := flag.Args()

	// Read the pod's untar'd bundle from a well-known path.
	cfg, err := apimanifests.GetBundleFromDir(*podBundleRoot)
	if err != nil {
		log.Fatalf("Failed to read bundle")
	}

	// Output logs to a buffer instead of stdout.
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)

	var results []scapiv1alpha3.TestResult
	tests := e2e.Tests{T: &testing.T{}}
	allTests := strings.Join(lib.GetMethods(tests), " ")

	// Check if the tests to run are valid.
	for _, entrypoint := range entrypoints {
		if entrypoint == "all" {
			entrypoints = strings.Split(allTests, " ")
			break
		}
		if !strings.Contains(allTests, entrypoint) {
			log.Fatalf("Test %s does not exist", entrypoint)
		}
	}
	for _, entrypoint := range entrypoints {
		reflection, err := lib.Invoke(tests, entrypoint, cfg)
		if err != nil {
			log.Printf("Failed to invoke test: %v", err)
		}
		result := reflection.Interface().(scapiv1alpha3.TestResult)
		result.Log = logBuf.String()
		results = append(results, result)
	}

	// Convert scapiv1alpha3.TestResult to json.
	prettyJSON, err := json.MarshalIndent(lib.WrapResult(results), "", "    ")
	if err != nil {
		log.Fatalf("Failed to generate json")
	}
	fmt.Printf("%s\n", string(prettyJSON))
}
