// Command update-pipeline-approvals bulk-updates pre-deployment and post-deployment approvers
// (and their policies) across release pipelines under a given path.
package updatepipelineapprovals

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

type approver struct {
	id, displayName string
}

type approverFlags []approver

func (f *approverFlags) String() string {
	parts := make([]string, len(*f))
	for i, a := range *f {
		parts[i] = a.id + ":" + a.displayName
	}
	return strings.Join(parts, ",")
}
func (f *approverFlags) Set(value string) error {
	id, name, ok := strings.Cut(value, ":")
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if !ok || id == "" || name == "" {
		return fmt.Errorf("expected IDENTITY_ID:DISPLAY_NAME, got %q", value)
	}
	*f = append(*f, approver{id: id, displayName: name})
	return nil
}

type idFlags []string

func (f *idFlags) String() string { return strings.Join(*f, ",") }
func (f *idFlags) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("expected an identity id")
	}
	*f = append(*f, value)
	return nil
}

const usageEpilog = `
Examples:
  update-pipeline-approvals 'Example.Project\TEST' --phase pre --add 'a1b2:Jane Doe' --stage Production -y
  update-pipeline-approvals 'Example.Project\TEST' --phase post --remove a1b2 --dry-run
  update-pipeline-approvals 'Example.Project\TEST' --phase both --clear --add 'a1b2:Jane Doe' --add 'c3d4:John Roe'
  update-pipeline-approvals 'Example.Project\TEST' --timeout 1440 --creator-can-approve=false --stage Production
Arguments:
  --phase                   pre, post, or both (default: both).
  --add ID:NAME             Add an approver by Azure DevOps identity id; repeatable.
  --remove ID               Remove an approver by identity id; repeatable.
  --clear                   Remove every existing approver for the selected phase(s) first.
                             Combine with --add to replace the approver list outright.
  --timeout                 Approval timeout in minutes (0 = no timeout). -1 leaves it unchanged.
  --required-approver-count Approvers required out of the configured list. 0 means "all of them".
                             -1 leaves it unchanged.
  --creator-can-approve     true or false: whether the release creator may approve their own
                             release. Omit to leave unchanged.
  --skip-if-previous-approved  true or false: skip this stage's approval if it was auto-triggered
                             and the previous stage's approval already covered it. Omit to leave unchanged.
  --stage                   Only update the named stage (default: all stages).
  --filter                  Only pipeline names containing this text.
  --level                   PAT authorization level: read-write or manage (default: read-write).
  --dry-run                 Lists planned changes without saving.
  -y, --yes                 Skips the confirmation prompt.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-approvals", flag.ExitOnError)
	phase := fs.String("phase", "both", "Which approval list to update: pre, post, or both")
	adds := approverFlags{}
	fs.Var(&adds, "add", "IDENTITY_ID:DISPLAY_NAME approver to add; repeatable")
	removes := idFlags{}
	fs.Var(&removes, "remove", "Identity id to remove; repeatable")
	clear := fs.Bool("clear", false, "Remove every existing approver for the selected phase(s) first")
	timeout := fs.Int("timeout", -1, "Approval timeout in minutes (0 = no timeout); -1 leaves unchanged")
	requiredCount := fs.Int("required-approver-count", -1, `Approvers required out of the list (0 = "all"); -1 leaves unchanged`)
	creatorCanApprove := fs.String("creator-can-approve", "", "true or false; omit to leave unchanged")
	skipIfPreviousApproved := fs.String("skip-if-previous-approved", "", "true or false; omit to leave unchanged")
	stage := fs.String("stage", "", "Only update the named stage")
	filter := fs.String("filter", "", "Only pipeline names containing this text")
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists planned changes without saving")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Bulk-updates pre/post-deployment approvers and approval policies across release pipelines.")
		fmt.Fprintln(os.Stderr, "\nUsage: update-pipeline-approvals <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{
		"phase": true, "add": true, "remove": true, "timeout": true, "required-approver-count": true,
		"creator-can-approve": true, "skip-if-previous-approved": true, "stage": true, "filter": true, "level": true,
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	phases, err := parsePhases(*phase)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(2)
	}
	var creatorCanApproveSet, skipIfPreviousApprovedSet *bool
	if *creatorCanApprove != "" {
		b, err := strconv.ParseBool(*creatorCanApprove)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: --creator-can-approve must be true or false")
			os.Exit(2)
		}
		creatorCanApproveSet = &b
	}
	if *skipIfPreviousApproved != "" {
		b, err := strconv.ParseBool(*skipIfPreviousApproved)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: --skip-if-previous-approved must be true or false")
			os.Exit(2)
		}
		skipIfPreviousApprovedSet = &b
	}
	if len(adds) == 0 && len(removes) == 0 && !*clear && *timeout < 0 && *requiredCount < 0 && creatorCanApproveSet == nil && skipIfPreviousApprovedSet == nil {
		fmt.Fprintln(os.Stderr, "Error: specify at least one of --add, --remove, --clear, --timeout, --required-approver-count, --creator-can-approve, --skip-if-previous-approved.")
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	settings := policySettings{
		timeout: *timeout, requiredApproverCount: *requiredCount,
		creatorCanApprove: creatorCanApproveSet, skipIfPreviousApproved: skipIfPreviousApprovedSet,
	}
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		return apply(raw, *stage, phases, adds, removes, *clear, settings)
	}
	if err := batchupdate.Run(opts, "Automated approval settings update", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

type policySettings struct {
	timeout, requiredApproverCount int
	creatorCanApprove              *bool
	skipIfPreviousApproved         *bool
}

var phaseKeys = map[string]string{"pre": "preDeployApprovals", "post": "postDeployApprovals"}

func parsePhases(value string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pre":
		return []string{"pre"}, nil
	case "post":
		return []string{"post"}, nil
	case "both", "":
		return []string{"pre", "post"}, nil
	default:
		return nil, fmt.Errorf("--phase must be one of: pre, post, both")
	}
}

func apply(raw map[string]interface{}, stage string, phases []string, adds []approver, removes []string, clearAll bool, settings policySettings) []string {
	var changes []string
	batchupdate.EachEnvironment(raw, stage, func(name string, env map[string]interface{}) {
		for _, phase := range phases {
			key := phaseKeys[phase]
			existing, _ := env[key].(map[string]interface{})
			block := existing
			if block == nil {
				block = map[string]interface{}{}
			}
			label := fmt.Sprintf("[stage %s / %s]", name, phase)
			line := updateApprovers(block, label, adds, removes, clearAll)
			policyLines := updatePolicy(block, label, settings)
			if line == "" && len(policyLines) == 0 {
				continue
			}
			if line != "" {
				changes = append(changes, line)
			}
			changes = append(changes, policyLines...)
			if existing == nil {
				env[key] = block
			}
		}
	})
	sort.Strings(changes)
	return changes
}

func updateApprovers(block map[string]interface{}, label string, adds []approver, removes []string, clearAll bool) string {
	current := currentApprovers(block)
	wanted := append([]approver(nil), current...)
	if clearAll {
		wanted = nil
	}
	for _, id := range removes {
		wanted = removeApprover(wanted, id)
	}
	for _, a := range adds {
		if !hasApprover(wanted, a.id) {
			wanted = append(wanted, a)
		}
	}
	if sameApprovers(current, wanted) {
		return ""
	}
	block["approvals"] = buildApprovals(wanted)
	return fmt.Sprintf("%s approvers: %v -> %v", label, names(current), names(wanted))
}

func buildApprovals(wanted []approver) []interface{} {
	if len(wanted) == 0 {
		return []interface{}{map[string]interface{}{"rank": 1, "isAutomated": true, "isNotificationOn": false, "id": 0}}
	}
	out := make([]interface{}, len(wanted))
	for i, a := range wanted {
		out[i] = map[string]interface{}{
			"rank": i + 1, "isAutomated": false, "isNotificationOn": false, "id": 0,
			"approver": map[string]interface{}{"id": a.id, "displayName": a.displayName},
		}
	}
	return out
}

func currentApprovers(block map[string]interface{}) []approver {
	items, _ := block["approvals"].([]interface{})
	var out []approver
	for _, item := range items {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		who, ok := entry["approver"].(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := who["id"].(string)
		if id == "" {
			continue
		}
		name, _ := who["displayName"].(string)
		out = append(out, approver{id: id, displayName: name})
	}
	return out
}

func updatePolicy(block map[string]interface{}, label string, settings policySettings) []string {
	options, _ := block["approvalOptions"].(map[string]interface{})
	if options == nil {
		options = map[string]interface{}{}
		block["approvalOptions"] = options
	}
	var changes []string
	if settings.timeout >= 0 {
		if old := batchupdate.Int(options, "timeoutInMinutes"); old != settings.timeout {
			options["timeoutInMinutes"] = settings.timeout
			changes = append(changes, fmt.Sprintf("%s timeout (min): %d -> %d", label, old, settings.timeout))
		}
	}
	if settings.requiredApproverCount >= 0 {
		old, hadOld := options["requiredApproverCount"]
		var newValue interface{}
		if settings.requiredApproverCount == 0 {
			newValue = nil
		} else {
			newValue = settings.requiredApproverCount
		}
		if !hadOld || !equalCount(old, newValue) {
			options["requiredApproverCount"] = newValue
			changes = append(changes, fmt.Sprintf("%s required approver count: %v -> %v", label, old, newValue))
		}
	}
	if settings.creatorCanApprove != nil {
		old, _ := options["releaseCreatorCanBeApprover"].(bool)
		if old != *settings.creatorCanApprove {
			options["releaseCreatorCanBeApprover"] = *settings.creatorCanApprove
			changes = append(changes, fmt.Sprintf("%s release creator can approve: %v -> %v", label, old, *settings.creatorCanApprove))
		}
	}
	if settings.skipIfPreviousApproved != nil {
		old, _ := options["autoTriggeredAndPreviousEnvironmentApprovedCanBeSkipped"].(bool)
		if old != *settings.skipIfPreviousApproved {
			options["autoTriggeredAndPreviousEnvironmentApprovedCanBeSkipped"] = *settings.skipIfPreviousApproved
			changes = append(changes, fmt.Sprintf("%s skip if previous approved: %v -> %v", label, old, *settings.skipIfPreviousApproved))
		}
	}
	return changes
}

func equalCount(old, newValue interface{}) bool {
	if old == nil || newValue == nil {
		return old == nil && newValue == nil
	}
	oldInt, ok1 := toInt(old)
	newInt, ok2 := toInt(newValue)
	return ok1 && ok2 && oldInt == newInt
}
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

func hasApprover(values []approver, id string) bool {
	for _, a := range values {
		if a.id == id {
			return true
		}
	}
	return false
}
func removeApprover(values []approver, id string) []approver {
	out := values[:0]
	for _, a := range values {
		if a.id != id {
			out = append(out, a)
		}
	}
	return out
}
func sameApprovers(a, b []approver) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func names(values []approver) []string {
	out := make([]string, len(values))
	for i, a := range values {
		out[i] = a.displayName
	}
	return out
}
