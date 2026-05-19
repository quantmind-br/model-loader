package ui

import (
	"reflect"
	"testing"
)

func TestFormatArgs(t *testing.T) {
	tests := []struct {
		name string
		args map[string]string
		want []string
	}{
		{
			name: "audit example",
			args: map[string]string{
				"batch-size":    "4096",
				"cache-type-k":  "q8_0",
				"cache-type-v":  "q8_0",
				"cont-batching": "true",
				"ctx-size":      "131072",
			},
			want: []string{
				"--batch-size 4096",
				"--cache-type-k q8_0",
				"--cache-type-v q8_0",
				"--cont-batching true",
				"--ctx-size 131072",
			},
		},
		{
			name: "empty map",
			args: map[string]string{},
			want: nil,
		},
		{
			name: "empty value",
			args: map[string]string{
				"flash-attn": "",
				"ctx-size":   "4096",
			},
			want: []string{
				"--ctx-size 4096",
				"--flash-attn",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FormatArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}
