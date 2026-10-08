package app

import (
	"strings"
	"testing"
)

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
