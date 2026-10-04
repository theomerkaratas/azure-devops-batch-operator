// Package cliutil provides small helpers shared by the CLI commands.
package cliutil

import "strings"

// ReorderArgs moves recognized flags to the front of args so flag.Parse can find them
// regardless of where they appear relative to positional arguments (flag.Parse otherwise stops
// at the first non-flag token). valueFlags lists the flag names (without leading dashes) that
// consume the following token as their value; any --name=value flag is treated as boolean.
func ReorderArgs(args []string, valueFlags map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}

		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if valueFlags[name] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}
