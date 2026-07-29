package domain

import "testing"

func TestExitClass(t *testing.T) {
	zero := 0
	one := 1
	tests := []struct {
		name string
		ri   RunningInstance
		want string
	}{
		{
			name: "operator stop wins over signal",
			ri: RunningInstance{
				ExitReason: ExitReasonOperatorStop,
				ExitSignal: "SIGTERM",
			},
			want: "stopped",
		},
		{
			name: "signal crashes",
			ri:   RunningInstance{ExitSignal: "SIGKILL"},
			want: "crashed",
		},
		{
			name: "nonzero code crashes",
			ri:   RunningInstance{ExitCode: &one},
			want: "crashed",
		},
		{
			name: "zero code exits",
			ri:   RunningInstance{ExitCode: &zero},
			want: "exited",
		},
		{
			name: "missing exit data crashes",
			ri:   RunningInstance{},
			want: "crashed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitClass(tt.ri); got != tt.want {
				t.Errorf("ExitClass(%+v) = %q, want %q", tt.ri, got, tt.want)
			}
		})
	}
}
