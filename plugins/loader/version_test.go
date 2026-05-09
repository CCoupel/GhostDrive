package loader

import (
	"testing"
)

func TestVersionFromPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "moosefs windows amd64",
			input: "ghostdrive-moosefs-v1.5.0-windows-amd64.ghdp",
			want:  "1.5.0",
		},
		{
			name:  "webdav windows amd64",
			input: "ghostdrive-webdav-v1.1.2-windows-amd64.ghdp",
			want:  "1.1.2",
		},
		{
			name:  "webdav linux amd64",
			input: "ghostdrive-webdav-v0.6.0-linux-amd64.ghdp",
			want:  "0.6.0",
		},
		{
			name:  "short suffix ending with dot",
			input: "ghostdrive-webdav-v1.1.2.ghdp",
			want:  "1.1.2",
		},
		{
			name:  "full absolute path",
			input: "/some/path/ghostdrive-moosefs-v1.5.0-windows-amd64.ghdp",
			want:  "1.5.0",
		},
		{
			name:  "no version tag",
			input: "myplugin.ghdp",
			want:  "unknown",
		},
		{
			name:  "empty string",
			input: "",
			want:  "unknown",
		},
		{
			name:  "version prefix missing v",
			input: "ghostdrive-webdav-1.2.3-windows-amd64.ghdp",
			want:  "unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := versionFromPath(tc.input)
			if got != tc.want {
				t.Errorf("versionFromPath(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}
