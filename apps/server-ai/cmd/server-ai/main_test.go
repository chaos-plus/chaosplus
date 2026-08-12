package main

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestConfigCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-ai.yaml")
	generate, err := newRootCommand([]string{"config", "generate", "--output", path})
	if err != nil {
		t.Fatal(err)
	}
	generate.SetOut(new(bytes.Buffer))
	if err := generate.Execute(); err != nil {
		t.Fatal(err)
	}
	validate, err := newRootCommand([]string{"config", "validate", "--config", path})
	if err != nil {
		t.Fatal(err)
	}
	validate.SetOut(new(bytes.Buffer))
	if err := validate.Execute(); err != nil {
		t.Fatal(err)
	}
}
