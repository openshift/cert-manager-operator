package common

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateName sets the name on the given object.
func UpdateName(obj client.Object, name string) {
	obj.SetName(name)
}

// UpdateNamespace sets the namespace on the given object.
func UpdateNamespace(obj client.Object, newNamespace string) {
	obj.SetNamespace(newNamespace)
}

// UpdateResourceLabels sets the labels on the given object.
func UpdateResourceLabels(obj client.Object, labels map[string]string) {
	obj.SetLabels(labels)
}

// HasObjectChanged compares two objects of the same type and returns true if they differ.
// Returns false if the objects are not of the same type.
func HasObjectChanged(desired, fetched client.Object) bool {
	if reflect.TypeOf(desired) != reflect.TypeOf(fetched) {
		return false
	}
	return ObjectMetadataModified(desired, fetched)
}

// ObjectMetadataModified compares the labels of two objects and returns true if they differ.
func ObjectMetadataModified(desired, fetched client.Object) bool {
	return !reflect.DeepEqual(desired.GetLabels(), fetched.GetLabels())
}

// ContainsAnnotation checks if the given object has the specified annotation.
func ContainsAnnotation(obj client.Object, annotation string) bool {
	_, exist := obj.GetAnnotations()[annotation]
	return exist
}

// AddAnnotation adds an annotation to the object if it doesn't already exist.
// Returns true if the annotation was added, false if it already existed.
func AddAnnotation(obj client.Object, annotation, value string) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	if _, exist := annotations[annotation]; !exist {
		annotations[annotation] = value
		obj.SetAnnotations(annotations)
		return true
	}
	return false
}

// DecodeObjBytes decodes raw YAML/JSON bytes into a typed Kubernetes object.
// Panics on decode failure or type mismatch.
func DecodeObjBytes[T runtime.Object](codecs serializer.CodecFactory, gv schema.GroupVersion, objBytes []byte) T {
	obj, err := runtime.Decode(codecs.UniversalDecoder(gv), objBytes)
	if err != nil {
		panic(fmt.Sprintf("failed to decode object bytes for %T: %v", *new(T), err))
	}
	typed, ok := obj.(T)
	if !ok {
		panic(fmt.Sprintf("failed to convert decoded object to %T", *new(T)))
	}
	return typed
}
