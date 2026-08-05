package gitops

import (
	"errors"
	"testing"
)

func TestParseVersionOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Version
	}{
		{"plain", "git version 2.39.2\n", Version{2, 39, 2}},
		{"macos suffix", "git version 2.39.2 (Apple Git-143)\n", Version{2, 39, 2}},
		{"windows suffix", "git version 2.40.0.windows.1\n", Version{2, 40, 0}},
		{"no trailing newline", "git version 2.30.0", Version{2, 30, 0}},
		{"major minor only", "git version 2.30\n", Version{2, 30, 0}},
		{"crlf line ending", "git version 2.34.1\r\n", Version{2, 34, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVersionOutput(tt.in)
			if err != nil {
				t.Fatalf("parseVersionOutput(%q) returned error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("parseVersionOutput(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseVersionOutput_Unreadable(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"unrelated text", "not a git binary\n"},
		{"missing version word", "git 2.39.2\n"},
		{"non-numeric major", "git version x.y.z\n"},
		{"single component", "git version 2\n"},
		{"negative component", "git version -2.3.0\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseVersionOutput(tt.in)
			if !errors.Is(err, ErrVersionUnreadable) {
				t.Fatalf("parseVersionOutput(%q) error = %v, want ErrVersionUnreadable", tt.in, err)
			}
		})
	}
}

func TestVersion_LessAndAtLeast(t *testing.T) {
	tests := []struct {
		name string
		a, b Version
		less bool
	}{
		{"equal", Version{2, 30, 0}, Version{2, 30, 0}, false},
		{"lower major", Version{1, 99, 99}, Version{2, 0, 0}, true},
		{"lower minor", Version{2, 29, 99}, Version{2, 30, 0}, true},
		{"lower patch", Version{2, 30, 0}, Version{2, 30, 1}, true},
		{"higher", Version{2, 31, 0}, Version{2, 30, 0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Less(tt.b); got != tt.less {
				t.Fatalf("%s.Less(%s) = %v, want %v", tt.a, tt.b, got, tt.less)
			}
			if got := tt.a.AtLeast(tt.b); got != !tt.less {
				t.Fatalf("%s.AtLeast(%s) = %v, want %v", tt.a, tt.b, got, !tt.less)
			}
		})
	}
}

func TestVersion_String(t *testing.T) {
	if got, want := (Version{2, 30, 1}).String(), "2.30.1"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
