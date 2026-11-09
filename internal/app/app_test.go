package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestUnknownCommand(t *testing.T) {
	for _, tt := range []struct{ name, suggestion string }{
		{"create-folderx", "create-folder"},
		{"create-fodler", "create-folder"},
		{"--helpk", "--help"},
		{"versoin", "version"},
		{"config-pat", "config-path"},
		{"unrelated-command", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			unknownCommand(&output, tt.name)
			got := output.String()
			if !strings.Contains(got, "unknown command "+`"`+tt.name+`"`) || !strings.Contains(got, "Run `a22r help` for usage.") {
				t.Fatalf("unexpected error output: %s", got)
			}
			if tt.suggestion != "" {
				if !strings.Contains(got, "Did you mean "+`"`+tt.suggestion+`"?`) {
					t.Errorf("missing suggestion: %s", got)
				}
			} else if strings.Contains(got, "Did you mean") {
				t.Errorf("unexpected suggestion: %s", got)
			}
			if strings.Contains(got, "Commands:") {
				t.Errorf("unknown command printed full help: %s", got)
			}
		})
	}
}

func TestEveryCommandHasDescription(t *testing.T) {
	for name := range commands {
		description := strings.TrimSpace(commandDescriptions[name])
		if description == "" {
			t.Errorf("command %q has no help description", name)
		}
		if !strings.HasSuffix(description, ".") {
			t.Errorf("command %q description is not a sentence: %q", name, description)
		}
	}
	for name := range commandDescriptions {
		if _, ok := commands[name]; !ok {
			t.Errorf("description exists for unknown command %q", name)
		}
	}
}
