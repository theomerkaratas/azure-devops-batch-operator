// Command orchestrate-releases creates releases for many pipelines in controlled waves, optionally
// waiting for each deployment, retrying transient errors and stopping after failures.
package orchestratereleases

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  orchestrate-releases 'Example.Project\TEST' --wave-size 5 --wave-delay 60 --dry-run
  orchestrate-releases 'Example.Project\TEST' --wave-size 3 --concurrency 2 --wait --stop-on-failure -y
  orchestrate-releases 'Example.Project\TEST' --stage Production --wait --timeout 90 --retries 3 -y
Waves:
  Pipelines are ordered by path and name and split into waves of --wave-size. Within a wave at most
  --concurrency releases are created at the same time (default: the whole wave). --wave-delay seconds
  are waited between waves.
Waiting and failures:
  --wait            Wait until each release finishes deploying (up to --timeout minutes, polling every
                    --poll seconds). A rejected, partially succeeded or canceled stage, or a timeout, is a failure.
  --stop-on-failure Do not start further waves (or further releases in the current wave) after a failure.
Retries:
  Creating a release is retried up to --retries times (waiting --retry-delay seconds, doubling each time) on
  HTTP 429/5xx and network errors. Before a retry the pipeline's latest releases are checked for this run's
  tag so a release that was actually created is never duplicated. Each release description ends with that tag.
A summary of every pipeline is printed at the end; the exit code is 1 if anything failed.
`

type options struct {
	waveSize, concurrency, retries int
	waveDelay, retryDelay, poll    time.Duration
	timeout                        time.Duration
	wait, stopOnFailure            bool
	stages                         []string
	description, runTag            string
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("orchestrate-releases", flag.ExitOnError)
	var o options
	stages := multiFlag{}
	fs.Var(&stages, "stage", "Stage to start manually. May be given multiple times.")
	fs.IntVar(&o.waveSize, "wave-size", 5, "Pipelines per wave")
	fs.IntVar(&o.concurrency, "concurrency", 0, "Maximum simultaneous releases within a wave (default: wave size)")
	waveDelay := fs.Float64("wave-delay", 0, "Seconds to wait between waves")
	fs.BoolVar(&o.wait, "wait", false, "Wait for each release to finish deploying")
	timeout := fs.Float64("timeout", 60, "Minutes to wait for each release (with --wait)")
	poll := fs.Float64("poll", 15, "Seconds between status checks (with --wait)")
	fs.BoolVar(&o.stopOnFailure, "stop-on-failure", false, "Stop starting new releases after a failure")
	fs.IntVar(&o.retries, "retries", 2, "Retries for transient create errors")
	retryDelay := fs.Float64("retry-delay", 5, "Seconds before the first retry (doubles each retry)")
	fs.StringVar(&o.description, "description", "Triggered by azure-devops-batch-operator", "Description stored on each release")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Shows the waves without creating anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Creates releases for many pipelines in controlled deployment waves.")
		fmt.Fprintln(os.Stderr, "\nUsage: orchestrate-releases <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"stage": true, "wave-size": true, "concurrency": true, "wave-delay": true, "timeout": true, "poll": true, "retries": true, "retry-delay": true, "description": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	switch {
	case fs.NArg() != 1:
		fs.Usage()
		os.Exit(2)
	case o.waveSize < 1, o.concurrency < 0, o.retries < 0:
		fmt.Fprintln(os.Stderr, "Error: --wave-size must be at least 1; --concurrency and --retries must not be negative")
		os.Exit(2)
	case *waveDelay < 0, *retryDelay < 0, *timeout <= 0, *poll <= 0:
		fmt.Fprintln(os.Stderr, "Error: delays must not be negative; --timeout and --poll must be positive")
		os.Exit(2)
	case !batchupdate.ValidWriteLevel(*level):
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if o.concurrency == 0 || o.concurrency > o.waveSize {
		o.concurrency = o.waveSize
	}
	o.waveDelay = time.Duration(*waveDelay * float64(time.Second))
	o.retryDelay = time.Duration(*retryDelay * float64(time.Second))
	o.poll = time.Duration(*poll * float64(time.Second))
	o.timeout = time.Duration(*timeout * float64(time.Minute))
	o.stages = []string(stages)
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	o.runTag = "[a22r-run " + hex.EncodeToString(buf) + "]"
	if err := run(fs.Arg(0), *filter, *level, o, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// result is the outcome for one pipeline.
type result struct {
	pipeline string
	wave     int
	release  string
	status   string // succeeded, idle, failed, create-failed, timeout, created, not-started
	detail   string
}

// api is what the orchestrator needs from Azure DevOps; tests replace it.
type api interface {
	create(defID int, description string, stages []string) (azuredevops.Release, error)
	recent(defID, top int) ([]azuredevops.Release, error)
	get(releaseID int) (azuredevops.Release, error)
}

type apiClient struct {
	cfg     azuredevops.Config
	project string
}

func (a apiClient) create(id int, d string, s []string) (azuredevops.Release, error) {
	return a.cfg.CreateRelease(a.project, id, d, s)
}
func (a apiClient) recent(id, top int) ([]azuredevops.Release, error) {
	return a.cfg.ListReleases(a.project, id, top)
}
func (a apiClient) get(id int) (azuredevops.Release, error) { return a.cfg.GetRelease(a.project, id) }

func run(target, filter, level string, o options, dryRun, autoYes bool) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, defs, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		fmt.Printf("No matching release pipelines found under `%s`.\n", target)
		return nil
	}
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].Path != defs[j].Path {
			return defs[i].Path < defs[j].Path
		}
		return defs[i].Name < defs[j].Name
	})
	waves := planWaves(defs, o.waveSize)
	fmt.Printf("Target: %s\nPipelines found: %d\nWaves: %d (size %d, concurrency %d)\n", target, len(defs), len(waves), o.waveSize, o.concurrency)
	if o.wait {
		fmt.Printf("Waiting for each deployment (timeout %s)\n", o.timeout)
	}
	if len(o.stages) > 0 {
		fmt.Printf("Manual stages: %s\n", strings.Join(o.stages, ", "))
	}
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no releases will be created) <<<")
	}
	fmt.Println("\n=== WAVES ===")
	for i, w := range waves {
		fmt.Printf("\nWave %d:\n", i+1)
		for _, d := range w {
			fmt.Printf("  %s\\%s (id=%d)\n", strings.TrimRight(d.Path, `\`), d.Name, d.ID)
		}
	}
	if dryRun {
		fmt.Println("\nDry-run complete. No releases were created.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\n%d release(s) will be created in %d wave(s). Continue? (y/N): ", len(defs), len(waves))) {
		fmt.Println("Cancelled.")
		return nil
	}
	results := orchestrate(apiClient{cfg, project}, waves, o, time.Sleep)
	failed := summarize(results)
	if failed > 0 {
		return fmt.Errorf("%d pipeline(s) failed or were not started", failed)
	}
	return nil
}

func planWaves(defs []azuredevops.ReleaseDefinition, size int) [][]azuredevops.ReleaseDefinition {
	var waves [][]azuredevops.ReleaseDefinition
	for i := 0; i < len(defs); i += size {
		end := i + size
		if end > len(defs) {
			end = len(defs)
		}
		waves = append(waves, defs[i:end])
	}
	return waves
}

// orchestrate runs the waves and returns one result per pipeline, in plan order.
func orchestrate(a api, waves [][]azuredevops.ReleaseDefinition, o options, sleep func(time.Duration)) []result {
	var results []result
	stopped := false
	var mu sync.Mutex
	for wi, wave := range waves {
		if wi > 0 && !stopped && o.waveDelay > 0 {
			fmt.Printf("\nWaiting %s before wave %d...\n", o.waveDelay, wi+1)
			sleep(o.waveDelay)
		}
		fmt.Printf("\n--- Wave %d/%d ---\n", wi+1, len(waves))
		waveResults := make([]result, len(wave))
		sem := make(chan struct{}, o.concurrency)
		var wg sync.WaitGroup
		for i, d := range wave {
			name := fmt.Sprintf(`%s\%s`, strings.TrimRight(d.Path, `\`), d.Name)
			waveResults[i] = result{pipeline: name, wave: wi + 1, status: "not-started", detail: "an earlier failure stopped the run"}
			sem <- struct{}{}
			mu.Lock()
			skip := stopped
			mu.Unlock()
			if skip {
				<-sem
				continue
			}
			wg.Add(1)
			go func(i int, d azuredevops.ReleaseDefinition, name string) {
				defer wg.Done()
				defer func() { <-sem }()
				r := deployOne(a, d, name, wi+1, o, sleep)
				waveResults[i] = r
				if o.stopOnFailure && isFailure(r.status) {
					mu.Lock()
					stopped = true
					mu.Unlock()
				}
			}(i, d, name)
		}
		wg.Wait()
		results = append(results, waveResults...)
	}
	return results
}

func isFailure(status string) bool {
	return status == "failed" || status == "create-failed" || status == "timeout" || status == "not-started"
}

// deployOne creates a release (with retries) and optionally waits for its deployment.
func deployOne(a api, d azuredevops.ReleaseDefinition, name string, wave int, o options, sleep func(time.Duration)) result {
	res := result{pipeline: name, wave: wave}
	description := strings.TrimSpace(o.description + " " + o.runTag)
	var rel azuredevops.Release
	var err error
	delay := o.retryDelay
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if found := findTagged(a, d.ID, o.runTag); found != nil {
				rel, err = *found, nil
				break
			}
		}
		rel, err = a.create(d.ID, description, o.stages)
		if err == nil || attempt >= o.retries || !isTransient(err) {
			break
		}
		fmt.Printf("  ! %s: transient error (%v); retry %d/%d in %s\n", name, err, attempt+1, o.retries, delay)
		sleep(delay)
		delay *= 2
	}
	if err != nil {
		res.status, res.detail = "create-failed", err.Error()
		fmt.Printf("  x %s: create failed: %v\n", name, err)
		return res
	}
	res.release = rel.Name
	fmt.Printf("  ok %s -> %s (id=%d)\n", name, rel.Name, rel.ID)
	if !o.wait {
		res.status = "created"
		return res
	}
	res.status, res.detail = waitForRelease(a, rel.ID, o, sleep)
	fmt.Printf("  %s %s: %s %s\n", marker(res.status), name, res.status, res.detail)
	return res
}

func marker(status string) string {
	if isFailure(status) {
		return "x"
	}
	return "ok"
}

// findTagged returns a recent release of the pipeline that carries this run's tag.
func findTagged(a api, defID int, tag string) *azuredevops.Release {
	releases, err := a.recent(defID, 5)
	if err != nil {
		return nil
	}
	for i := range releases {
		if strings.Contains(releases[i].Description, tag) {
			return &releases[i]
		}
	}
	return nil
}

// isTransient reports whether an API error is worth retrying: throttling, server errors and network errors.
func isTransient(err error) bool {
	msg := err.Error()
	if i := strings.Index(msg, "(HTTP "); i >= 0 {
		code := msg[i+len("(HTTP "):]
		return strings.HasPrefix(code, "429") || strings.HasPrefix(code, "5")
	}
	lower := strings.ToLower(msg)
	for _, s := range []string{"timeout", "connection reset", "connection refused", "eof", "temporary", "no such host", "tls handshake"} {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// deploymentOutcome classifies a release's stage statuses: done reports whether nothing is still running.
func deploymentOutcome(r azuredevops.Release) (outcome string, done bool) {
	succeeded, failed, active, started := false, false, false, false
	for _, e := range r.Environments {
		switch strings.ToLower(e.Status) {
		case "inprogress", "queued", "scheduled":
			active, started = true, true
		case "succeeded":
			succeeded, started = true, true
		case "rejected", "partiallysucceeded", "canceled":
			failed, started = true, true
		}
	}
	switch {
	case active:
		return "", false
	case failed:
		return "failed", true
	case succeeded:
		return "succeeded", true
	case started:
		return "idle", true
	}
	return "idle", true
}

// waitForRelease polls until the release is done, fails, or the timeout is reached. The first
// quiet check is repeated once so stages that start moments after creation are not missed.
func waitForRelease(a api, id int, o options, sleep func(time.Duration)) (status, detail string) {
	var waited time.Duration
	quiet := 0
	for {
		rel, err := a.get(id)
		if err == nil {
			outcome, done := deploymentOutcome(rel)
			switch {
			case done && outcome == "failed":
				return "failed", describeFailure(rel)
			case done:
				quiet++
				if quiet >= 2 {
					if outcome == "idle" {
						return "idle", "no stage deployed (manual or waiting stages only)"
					}
					return "succeeded", ""
				}
			default:
				quiet = 0
			}
		} else if !isTransient(err) {
			return "failed", "status check failed: " + err.Error()
		}
		if waited >= o.timeout {
			return "timeout", fmt.Sprintf("still deploying after %s", o.timeout)
		}
		sleep(o.poll)
		waited += o.poll
	}
}

func describeFailure(r azuredevops.Release) string {
	var bad []string
	for _, e := range r.Environments {
		switch strings.ToLower(e.Status) {
		case "rejected", "partiallysucceeded", "canceled":
			bad = append(bad, e.Name+" "+e.Status)
		}
	}
	return strings.Join(bad, ", ")
}

// summarize prints the final table and returns the number of failed or unstarted pipelines.
func summarize(results []result) int {
	fmt.Println("\n=== DEPLOYMENT SUMMARY ===")
	counts := map[string]int{}
	failed := 0
	for _, r := range results {
		counts[r.status]++
		if isFailure(r.status) {
			failed++
		}
		line := fmt.Sprintf("  wave %d  %-14s %s", r.wave, r.status, r.pipeline)
		if r.release != "" {
			line += " -> " + r.release
		}
		if r.detail != "" {
			line += "  (" + r.detail + ")"
		}
		fmt.Println(line)
	}
	var parts []string
	for _, s := range []string{"succeeded", "created", "idle", "failed", "create-failed", "timeout", "not-started"} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", s, counts[s]))
		}
	}
	fmt.Printf("\nTotal: %d | %s\n", len(results), strings.Join(parts, " | "))
	return failed
}
