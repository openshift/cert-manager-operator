//nolint:revive // package name "common" is required and cannot be changed
package common

// ManagedResourceLabelKey is the common label key used by all operand controllers
// to identify resources they manage. Each controller uses a different value
// to distinguish its resources.
const ManagedResourceLabelKey = "app"
