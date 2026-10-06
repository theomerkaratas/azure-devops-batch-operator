// Command list-pool-members lists the agents that belong to matching Azure DevOps agent pools.
package listpoolmembers

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
  list-pool-members TestPool
  list-pool-members --level manage
Arguments:
  pool     Optional. Pool name or part of it (case-insensitive). Empty matches every pool.
  --level  Optional. The PAT authorization level to use (default: read).
           Choices: read | read-write | manage
           The selected PAT must have agent pool read access.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("list-pool-members", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Lists the agents that belong to matching Azure DevOps agent pools.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: list-pool-members [pool] [--level read|read-write|manage]")
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
	if err := run(fs.Arg(0), *level); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(filter, level string) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	pools, err := cfg.ListPools()
	if err != nil {
		return err
	}

	filter = strings.ToLower(strings.TrimSpace(filter))
	matched := 0
	memberCount := 0
	failed := 0
	for _, pool := range pools {
		if filter != "" && !strings.Contains(strings.ToLower(pool.Name), filter) {
			continue
		}
		matched++
		fmt.Printf("\nPool: %s (id=%d)\n", pool.Name, pool.ID)
		agents, err := cfg.ListPoolAgents(pool.ID)
		if err != nil {
			fmt.Printf("  error: %v\n", err)
			failed++
			continue
		}
		if len(agents) == 0 {
			fmt.Println("  (no members)")
			continue
		}
		for _, agent := range agents {
			state := agent.Status
			if !agent.Enabled {
				state += ", disabled"
			}
			fmt.Printf("  - %-35s %-18s v%s  %s\n", agent.Name, state, agent.Version, agent.OSDescription)
			memberCount++
		}
	}

	if matched == 0 {
		fmt.Printf("No pools found matching %q.\n", filter)
		return nil
	}
	if failed > 0 {
		return fmt.Errorf("could not list members for %d of %d matching pool(s)", failed, matched)
	}
	fmt.Printf("\n%d member(s) across %d pool(s).\n", memberCount, matched)
	return nil
}
