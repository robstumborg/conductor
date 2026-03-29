package agent

import (
	"reflect"
	"testing"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"build", false},
		{"plan", false},
		{"code-reviewer", false},
		{"", true},
		{"build agent", true},
	}
	for _, tt := range tests {
		err := ValidateName(tt.value)
		if (err != nil) != tt.wantErr {
			t.Fatalf("ValidateName(%q) error=%v wantErr=%v", tt.value, err, tt.wantErr)
		}
	}
}

func TestListAvailable(t *testing.T) {
	origLookPath := lookPath
	origOutput := commandOutput
	defer func() {
		lookPath = origLookPath
		commandOutput = origOutput
	}()

	lookPath = func(string) (string, error) { return "/usr/bin/opencode", nil }
	commandOutput = func(name string, args ...string) ([]byte, error) {
		return []byte("build (primary)\n  []\nplan (primary)\n  []\ncode-reviewer (subagent)\n  []\n"), nil
	}

	got, err := ListAvailable("opencode")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"build", "plan", "code-reviewer"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agents=%v want=%v", got, want)
	}
}

func TestValidateAvailable(t *testing.T) {
	origLookPath := lookPath
	origOutput := commandOutput
	defer func() {
		lookPath = origLookPath
		commandOutput = origOutput
	}()

	lookPath = func(string) (string, error) { return "/usr/bin/opencode", nil }
	commandOutput = func(name string, args ...string) ([]byte, error) {
		return []byte("build (primary)\n  []\nplan (primary)\n  []\n"), nil
	}

	if err := ValidateAvailable("opencode", "build"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAvailable("opencode", "bad agent"); err == nil {
		t.Fatal("expected format error")
	}
	if err := ValidateAvailable("opencode", "missing"); err == nil {
		t.Fatal("expected availability error")
	}
}

func TestResolve(t *testing.T) {
	if got := Resolve("build", "", ""); got != "build" {
		t.Fatalf("got %q", got)
	}
	if got := Resolve("build", "plan", ""); got != "plan" {
		t.Fatalf("got %q", got)
	}
	if got := Resolve("build", "plan", "review"); got != "review" {
		t.Fatalf("got %q", got)
	}
}
