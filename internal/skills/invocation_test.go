package skills

import (
	"errors"
	"testing"
)

func TestParseInvocation(t *testing.T) {
	t.Parallel()

	catalog := []Skill{
		{Name: "oracle-dba"},
		{Name: "oracle-dba-extended"},
		{Name: "build"},
	}

	tests := []struct {
		name      string
		content   string
		want      Invocation
		wantMatch bool
	}{
		{name: "ordinary text", content: "please inspect the database"},
		{name: "missing slash", content: "oracle-dba 10.102.78.1"},
		{
			name:      "exact skill",
			content:   "/oracle-dba",
			want:      Invocation{Name: "oracle-dba"},
			wantMatch: true,
		},
		{
			name:      "skill with arguments",
			content:   "/oracle-dba 10.102.78.1",
			want:      Invocation{Name: "oracle-dba", Args: "10.102.78.1"},
			wantMatch: true,
		},
		{
			name:      "trims argument whitespace",
			content:   "  /oracle-dba   10.102.78.1  ",
			want:      Invocation{Name: "oracle-dba", Args: "10.102.78.1"},
			wantMatch: true,
		},
		{
			name:      "longest known prefix without separator",
			content:   "/oracle-dba-extended10.102.78.1",
			want:      Invocation{Name: "oracle-dba-extended", Args: "10.102.78.1"},
			wantMatch: true,
		},
		{
			name:      "case sensitive",
			content:   "/Oracle-DBA 10.102.78.1",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, matched, err := ParseInvocation(tt.content, catalog)
			if err != nil {
				t.Fatalf("ParseInvocation() error = %v", err)
			}
			if matched != tt.wantMatch {
				t.Fatalf("ParseInvocation() matched = %v, want %v", matched, tt.wantMatch)
			}
			if got != tt.want {
				t.Fatalf("ParseInvocation() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseInvocationRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	_, matched, err := ParseInvocation("/build", []Skill{{Name: "build"}, {Name: "build"}})
	if !matched {
		t.Fatal("ParseInvocation() matched = false, want true for duplicate catalog entry")
	}
	if !errors.Is(err, ErrAmbiguousInvocation) {
		t.Fatalf("ParseInvocation() error = %v, want ErrAmbiguousInvocation", err)
	}
}

func TestParseInvocationDuplicateDoesNotShadowLongerUniqueMatch(t *testing.T) {
	t.Parallel()

	// Regression: the duplicate-name check used to short-circuit before the
	// longest-match scan, so the duplicated short name "build" reported a
	// spurious ambiguity for "/build-tool x" instead of matching build-tool.
	catalog := []Skill{
		{Name: "build"},
		{Name: "build"},
		{Name: "build-tool"},
	}
	got, matched, err := ParseInvocation("/build-tool x", catalog)
	if err != nil {
		t.Fatalf("ParseInvocation() error = %v, want nil", err)
	}
	if !matched {
		t.Fatal("ParseInvocation() matched = false, want true")
	}
	want := Invocation{Name: "build-tool", Args: "x"}
	if got != want {
		t.Fatalf("ParseInvocation() = %+v, want %+v", got, want)
	}
}

func TestParseInvocationAmbiguityFollowsLongestMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		catalog []Skill
		content string
	}{
		{
			name:    "duplicated long name stays ambiguous",
			catalog: []Skill{{Name: "build"}, {Name: "build-tool"}, {Name: "build-tool"}},
			content: "/build-tool x",
		},
		{
			name:    "duplicated compact-only match stays ambiguous",
			catalog: []Skill{{Name: "build"}, {Name: "build"}},
			content: "/build-tool x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, matched, err := ParseInvocation(tt.content, tt.catalog)
			if !matched {
				t.Fatal("ParseInvocation() matched = false, want true")
			}
			if !errors.Is(err, ErrAmbiguousInvocation) {
				t.Fatalf("ParseInvocation() error = %v, want ErrAmbiguousInvocation", err)
			}
		})
	}
}
