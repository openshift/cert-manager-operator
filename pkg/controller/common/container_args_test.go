package common

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func TestMergeContainerArgs(t *testing.T) {
	tests := []struct {
		name         string
		sourceArgs   []string
		overrideArgs []string
		expected     []string
	}{
		{
			name:         "merge args overrides keys and sorts",
			sourceArgs:   []string{"--a=12"},
			overrideArgs: []string{"A", "B", "--a=vc"},
			expected:     []string{"--a=vc", "A", "B"},
		},
		{
			name:         "overrideargs replaces source arg values",
			sourceArgs:   []string{"--key1=value1", "--key2=value2"},
			overrideArgs: []string{"--key1=value1", "--key2=value5"},
			expected:     []string{"--key1=value1", "--key2=value5"},
		},
		{
			name:         "after merge, args are sorted in increasing order",
			sourceArgs:   []string{"--xxx1=value1", "--xyz=value2"},
			overrideArgs: []string{"--def=value1", "--abc=value5"},
			expected:     []string{"--abc=value5", "--def=value1", "--xxx1=value1", "--xyz=value2"},
		},
		{
			name:         "after merge, duplicates are removed",
			sourceArgs:   []string{"--abc=value1", "", "--xyz=value2"},
			overrideArgs: []string{"--xyz=value1", "--abc=value1"},
			expected:     []string{"--abc=value1", "--xyz=value1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualArgs := MergeContainerArgs(tc.sourceArgs, tc.overrideArgs)
			require.Equal(t, tc.expected, actualArgs)
		})
	}
}

func TestParseArgMap(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMap map[string]string
	}{
		{
			name: "parses keys, empty token, and multi-segment values",
			args: []string{
				"", // should be ignored at the time of parse
				"--", "--foo", "--v=1", "--test=v1=v2", "--gates=Feature1=True",
				"--log-level=Debug=false,Info=false,Warning=True,Error=true",
				"--extra-flags='--v=2 --gates=Feature2=True'",
			},
			wantMap: map[string]string{
				"--":            "",
				"--foo":         "",
				"--v":           "1",
				"--test":        "v1=v2",
				"--gates":       "Feature1=True",
				"--log-level":   "Debug=false,Info=false,Warning=True,Error=true",
				"--extra-flags": "'--v=2 --gates=Feature2=True'",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argMap := make(map[string]string)
			ParseArgMap(argMap, tt.args)
			if !reflect.DeepEqual(argMap, tt.wantMap) {
				t.Fatalf("unexpected update to arg map, diff = %v", cmp.Diff(tt.wantMap, argMap))
			}
		})
	}
}

func TestStripArgsByKeys(t *testing.T) {
	keys := ArgKeysSet([]string{"--tls-cipher-suites", "--tls-min-version"})
	args := []string{
		"--tls-min-version=VersionTLS12",
		"--tls-cipher-suites=ECDHE-RSA-AES128-GCM-SHA256",
		"--v=2",
	}
	got := StripArgsByKeys(args, keys)
	require.Equal(t, []string{"--v=2"}, got)
	require.Equal(t, args, StripArgsByKeys(args, nil))
}
