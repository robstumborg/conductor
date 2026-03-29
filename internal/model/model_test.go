package model

import (
	"reflect"
	"testing"
)

func TestValidateFormat(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"openai/gpt-5.4", false},
		{"gpt-5.4", true},
		{"openai/", true},
		{"/gpt-5.4", true},
		{"", true},
	}
	for _, tt := range tests {
		err := ValidateFormat(tt.value)
		if (err != nil) != tt.wantErr {
			t.Fatalf("ValidateFormat(%q) error=%v wantErr=%v", tt.value, err, tt.wantErr)
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
		return []byte("openai/gpt-5.4\nopenai/gpt-5.4-mini\n"), nil
	}

	got, err := ListAvailable("opencode")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"openai/gpt-5.4", "openai/gpt-5.4-mini"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models=%v want=%v", got, want)
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
		return []byte("openai/gpt-5.4\nopenai/gpt-5.4-mini\n"), nil
	}

	if err := ValidateAvailable("opencode", "openai/gpt-5.4"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAvailable("opencode", "gpt-5.4"); err == nil {
		t.Fatal("expected format error")
	}
	if err := ValidateAvailable("opencode", "openai/missing"); err == nil {
		t.Fatal("expected availability error")
	}
}

func TestResolve(t *testing.T) {
	if got := Resolve("openai/gpt-5.4", "", ""); got != "openai/gpt-5.4" {
		t.Fatalf("got %q", got)
	}
	if got := Resolve("openai/gpt-5.4", "openai/gpt-5.4-mini", ""); got != "openai/gpt-5.4-mini" {
		t.Fatalf("got %q", got)
	}
	if got := Resolve("openai/gpt-5.4", "openai/gpt-5.4-mini", "openai/gpt-5.2"); got != "openai/gpt-5.2" {
		t.Fatalf("got %q", got)
	}
}
