// Command list-pools lists agent pools, or the agents (members) of matching pools.
package listpools

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  list-pools                      # all pools
  list-pools TestPool             # members (agents) of pools whose name contains 'TestPool'
  list-pools --members            # members of every pool
Arguments:
  pool       Optional. Pool name (or part of it). When given, the members of the matching
             pools are listed with their status.
  --members  Optional. List the members of every pool.
  --level    Optional. The PAT authorization level to use (default: read).
             Choices: read | read-write | manage
             Note: the agent pools API may require a PAT with pool read access.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("list-pools", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	members := fs.Bool("members", false, "List the members of every pool")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Lists agent pools, or the agents (members) of matching pools.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: list-pools [pool] [--members] [--level read|read-write|manage]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], map[string]bool{"level": true})); err != nil {
		os.Exit(2)
	}
	if fs.NArg() > 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *members, *level); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func run(filter string, allMembers bool, level string) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	pools, err := cfg.ListPools()
	if err != nil {
		return err
	}
	var matched []azuredevops.Pool
	for _, p := range pools {
		if filter == "" || strings.Contains(strings.ToLower(p.Name), strings.ToLower(filter)) {
			matched = append(matched, p)
		}
	}
	if len(matched) == 0 {
		fmt.Printf("No pools found matching '%s'.\n", filter)
		return nil
	}
	listMembers := allMembers || filter != ""
	if !listMembers {
		for _, p := range matched {
			fmt.Printf("%-40s id=%-5d agents=%d hosted=%v\n", p.Name, p.ID, p.Size, p.IsHosted)
		}
		fmt.Printf("\n%d pool(s).\n", len(matched))
		return nil
	}
	for _, p := range matched {
		fmt.Printf("\nPool: %s (id=%d)\n", p.Name, p.ID)
		agents, err := cfg.ListPoolAgents(p.ID)
		if err != nil {
			fmt.Printf("  error: %v\n", err)
			continue
		}
		if len(agents) == 0 {
			fmt.Println("  (no agents)")
		}
		for _, a := range agents {
			state := a.Status
			if !a.Enabled {
				state += ", disabled"
			}
			fmt.Printf("  - %-35s %-18s v%s  %s\n", a.Name, state, a.Version, a.OSDescription)
		}
	}
	return nil
}
