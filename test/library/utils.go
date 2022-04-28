package library

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	scapiv1alpha3 "github.com/operator-framework/api/pkg/apis/scorecard/v1alpha3"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (d DynamicResourceLoader) CreateTestingNS(baseName string) (*v1.Namespace, error) {
	name := fmt.Sprintf("%v", baseName)
	t := testing.T{}
	namespaceObj := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "",
		},
		Status: v1.NamespaceStatus{},
	}

	var got *v1.Namespace
	if err := wait.PollImmediate(1*time.Second, 30*time.Second, func() (bool, error) {
		var err error
		got, err = d.KubeClient.CoreV1().Namespaces().Create(context.Background(), namespaceObj, metav1.CreateOptions{})
		if err != nil {
			t.Logf("Error creating namespace: %v", err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	return got, nil
}

func (d DynamicResourceLoader) DeleteTestingNS(baseName string) (bool, error) {
	name := fmt.Sprintf("%v", baseName)
	t := testing.T{}
	ctx := context.Background()

	err := d.KubeClient.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		t.Logf("Namespace: %v not found, err: %v", name, err)
	}

	if err := wait.PollImmediate(1*time.Second, 30*time.Second, func() (bool, error) {

		// Poll until namespace is deleted
		ns, err := d.KubeClient.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		t.Logf("Namespace: %v", ns)
		if err != nil {
			t.Logf("Error getting namespace: %v", err)
			if k8serrors.IsNotFound(err) {
				return true, err
			}
			return false, nil
		}
		return false, nil
	}); err != nil {
		t.Logf("Error getting namespace: %v", err)
		return true, err
	}
	return false, nil
}

func WrapResult(results []scapiv1alpha3.TestResult) scapiv1alpha3.TestStatus {
	return scapiv1alpha3.TestStatus{
		Results: results,
	}
}

func Invoke(any interface{}, name string, args ...interface{}) (reflect.Value, error) {
	method := reflect.ValueOf(any).MethodByName(name)
	methodType := method.Type()
	numIn := methodType.NumIn()
	if numIn > len(args) {
		return reflect.ValueOf(nil), fmt.Errorf("Method %s must have minimum %d params. Have %d", name, numIn, len(args))
	}
	if numIn != len(args) && !methodType.IsVariadic() {
		return reflect.ValueOf(nil), fmt.Errorf("Method %s must have %d params. Have %d", name, numIn, len(args))
	}
	in := make([]reflect.Value, len(args))
	for i := 0; i < len(args); i++ {
		var inType reflect.Type
		if methodType.IsVariadic() && i >= numIn-1 {
			inType = methodType.In(numIn - 1).Elem()
		} else {
			inType = methodType.In(i)
		}
		argValue := reflect.ValueOf(args[i])
		if !argValue.IsValid() {
			return reflect.ValueOf(nil), fmt.Errorf("Method %s. Param[%d] must be %s. Have %s", name, i, inType, argValue.String())
		}
		argType := argValue.Type()
		if argType.ConvertibleTo(inType) {
			in[i] = argValue.Convert(inType)
		} else {
			return reflect.ValueOf(nil), fmt.Errorf("Method %s. Param[%d] must be %s. Have %s", name, i, inType, argType)
		}
	}
	return method.Call(in)[0], nil
}

// Get all methods by struct
func GetMethods(any interface{}) []string {
	var methods []string
	t := reflect.TypeOf(any)
	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		methods = append(methods, method.Name)
	}
	return methods
}
