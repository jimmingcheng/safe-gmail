package main

import "testing"

func TestJoinQueryArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{
			name: "single query arg",
			args: []string{`to:sidfishes6@comcast.net subject:"payment record"`},
			want: `to:sidfishes6@comcast.net subject:"payment record"`,
		},
		{
			name: "multiple query args",
			args: []string{"label:donna", "newer_than:7d"},
			want: "label:donna newer_than:7d",
		},
		{
			name:    "double dash after query is rejected",
			args:    []string{`to:sidfishes6@comcast.net subject:"payment record"`, "--limit", "50"},
			wantErr: true,
		},
		{
			name: "single dash query token remains valid",
			args: []string{"from:jimming@gmail.com", "-label:spam"},
			want: "from:jimming@gmail.com -label:spam",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := joinQueryArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("joinQueryArgs(%q) error = nil, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("joinQueryArgs(%q) error = %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("joinQueryArgs(%q) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}
