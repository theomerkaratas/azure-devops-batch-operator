// Command list-batch-manifests lists the saved manifests of batch operations, or shows one.
package listbatchmanifests

import (
	"flag"
	"fmt"
	"os"

	"github.com/omerkaratas/azure-devops-go-automations/internal/manifest"
)

const usageEpilog = `
Examples:
  list-batch-manifests
  list-batch-manifests 20260101-120000-ab12cd
Manifests are stored beside the configuration file (override with A22R_MANIFEST_DIR).
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("list-batch-manifests", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Lists saved batch operation manifests, or shows one in detail.")
		fmt.Fprintln(os.Stderr, "\nUsage: list-batch-manifests [manifest-id]")
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() > 1 {
		fs.Usage()
		os.Exit(2)
	}
	var err error
	if fs.NArg() == 1 {
		err = show(fs.Arg(0))
	} else {
		err = list()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func list() error {
	all, err := manifest.List()
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Println("No manifests saved.")
		return nil
	}
	for _, m := range all {
		c := m.Counts()
		fmt.Printf("%s  %s  %-28s %s  ok=%d failed=%d pending=%d rolled-back=%d\n", m.ID, m.Created.Local().Format("2006-01-02 15:04"),
			m.Command, m.Target, c[manifest.StatusSucceeded], c[manifest.StatusFailed], c[manifest.StatusPending], c[manifest.StatusRolledBack])
	}
	return nil
}

func show(id string) error {
	m, err := manifest.Load(id)
	if err != nil {
		return err
	}
	fmt.Printf("Manifest : %s\nFile     : %s\nCommand  : %s\nTarget   : %s\nFilter   : %s\nProject  : %s\nCreated  : %s\n",
		m.ID, m.Path(), m.Command, m.Target, m.Filter, m.Project, m.Created.Local().Format("2006-01-02 15:04:05"))
	for _, e := range m.Entries {
		fmt.Printf("\n[%s] %s\\%s (id=%d, original rev %d", e.Status, e.Path, e.Name, e.DefinitionID, e.OriginalRevision)
		if e.NewRevision != 0 {
			fmt.Printf(", new rev %d", e.NewRevision)
		}
		if e.RolledBackRev != 0 {
			fmt.Printf(", rolled back to rev %d", e.RolledBackRev)
		}
		fmt.Println(")")
		for _, l := range e.Changes {
			fmt.Printf("  %s\n", l)
		}
		if e.Error != "" {
			fmt.Printf("  error: %s\n", e.Error)
		}
	}
	return nil
}
