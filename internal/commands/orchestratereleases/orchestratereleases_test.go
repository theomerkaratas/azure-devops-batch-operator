package orchestratereleases

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

type fakeAPI struct {
	mu        sync.Mutex
	createErr map[int][]error // errors returned on successive create calls per definition
	calls     map[int]int
	created   map[int]azuredevops.Release
	envs      map[int][]string // statuses returned by successive get calls per release id
	gets      map[int]int
	maxActive int
	active    int
}

func (f *fakeAPI) create(id int, desc string, _ []string) (azuredevops.Release, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	n := f.calls[id]
	f.calls[id]++
	f.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	f.mu.Lock()
	f.active--
	defer f.mu.Unlock()
	if errs := f.createErr[id]; n < len(errs) && errs[n] != nil {
		if errors.Is(errs[n], errCreatedButFailed) {
			f.created[id] = azuredevops.Release{ID: id * 100, Name: "R", Description: desc}
		}
		return azuredevops.Release{}, errs[n]
	}
	return azuredevops.Release{ID: id * 100, Name: "Release-1", Description: desc}, nil
}

var errCreatedButFailed = errors.New("request failed (HTTP 503): boom")

func (f *fakeAPI) recent(id, _ int) ([]azuredevops.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.created[id]; ok {
		return []azuredevops.Release{r}, nil
	}
	return nil, nil
}

func (f *fakeAPI) get(rid int) (azuredevops.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seq := f.envs[rid]
	i := f.gets[rid]
	f.gets[rid]++
	if i >= len(seq) {
		i = len(seq) - 1
	}
	return azuredevops.Release{ID: rid, Environments: []azuredevops.ReleaseEnvironmentStatus{{Name: "Prod", Status: seq[i]}}}, nil
}

func newFake() *fakeAPI {
	return &fakeAPI{createErr: map[int][]error{}, calls: map[int]int{}, created: map[int]azuredevops.Release{}, envs: map[int][]string{}, gets: map[int]int{}}
}

func defs(ids ...int) []azuredevops.ReleaseDefinition {
	var out []azuredevops.ReleaseDefinition
	for _, id := range ids {
		out = append(out, azuredevops.ReleaseDefinition{ID: id, Name: "P", Path: `\`})
	}
	return out
}

func opts() options {
	return options{waveSize: 2, concurrency: 2, retries: 2, retryDelay: time.Millisecond, poll: time.Millisecond, timeout: time.Second, runTag: "[a22r-run t]"}
}

var noSleep = func(time.Duration) {}

func TestPlanWaves(t *testing.T) {
	if w := planWaves(defs(1, 2, 3, 4, 5), 2); len(w) != 3 || len(w[2]) != 1 {
		t.Fatalf("%v", w)
	}
}

func TestIsTransient(t *testing.T) {
	yes := []error{errors.New("request failed (HTTP 429): slow"), errors.New("request failed (HTTP 503): x"), errors.New("read tcp: connection reset by peer"), errors.New("context deadline: timeout")}
	no := []error{errors.New("request failed (HTTP 400): bad"), errors.New("request failed (HTTP 404): none"), errors.New("request failed (HTTP 401): auth")}
	for _, e := range yes {
		if !isTransient(e) {
			t.Errorf("%v should be transient", e)
		}
	}
	for _, e := range no {
		if isTransient(e) {
			t.Errorf("%v should not be transient", e)
		}
	}
}

func TestRetryDoesNotDuplicateCreatedRelease(t *testing.T) {
	f := newFake()
	f.createErr[1] = []error{errCreatedButFailed} // server created it, then returned 503
	r := deployOne(f, defs(1)[0], "P", 1, opts(), noSleep)
	if r.status != "created" || f.calls[1] != 1 {
		t.Fatalf("status=%s calls=%d", r.status, f.calls[1])
	}
	f2 := newFake()
	f2.createErr[1] = []error{errors.New("request failed (HTTP 429): slow"), nil}
	if r := deployOne(f2, defs(1)[0], "P", 1, opts(), noSleep); r.status != "created" || f2.calls[1] != 2 {
		t.Fatalf("retry: %s %d", r.status, f2.calls[1])
	}
	f3 := newFake()
	f3.createErr[1] = []error{errors.New("request failed (HTTP 400): bad")}
	if r := deployOne(f3, defs(1)[0], "P", 1, opts(), noSleep); r.status != "create-failed" || f3.calls[1] != 1 {
		t.Fatalf("permanent: %s %d", r.status, f3.calls[1])
	}
}

func TestWaitOutcomes(t *testing.T) {
	f := newFake()
	f.envs[100] = []string{"notStarted", "inProgress", "succeeded"}
	o := opts()
	o.wait = true
	if r := deployOne(f, defs(1)[0], "P", 1, o, noSleep); r.status != "succeeded" {
		t.Fatalf("%s %s", r.status, r.detail)
	}
	f.envs[200] = []string{"inProgress", "rejected"}
	if r := deployOne(f, defs(2)[0], "P", 1, o, noSleep); r.status != "failed" {
		t.Fatalf("%s", r.status)
	}
	f.envs[300] = []string{"inProgress"}
	o.timeout = 3 * time.Millisecond
	if r := deployOne(f, defs(3)[0], "P", 1, o, noSleep); r.status != "timeout" {
		t.Fatalf("%s", r.status)
	}
}

func TestStopOnFailureSkipsLaterWavesAndConcurrencyIsBounded(t *testing.T) {
	f := newFake()
	f.createErr[1] = []error{errors.New("request failed (HTTP 400): bad")}
	o := opts()
	o.stopOnFailure = true
	results := orchestrate(f, planWaves(defs(1, 2, 3, 4), 2), o, noSleep)
	if len(results) != 4 {
		t.Fatalf("%d results", len(results))
	}
	if results[0].status != "create-failed" || results[2].status != "not-started" || results[3].status != "not-started" {
		t.Fatalf("%+v", results)
	}
	if summarize(results) != 3 {
		t.Fatal("failed count")
	}

	g := newFake()
	o2 := opts()
	o2.waveSize, o2.concurrency = 6, 2
	orchestrate(g, planWaves(defs(1, 2, 3, 4, 5, 6), 6), o2, noSleep)
	if g.maxActive > 2 {
		t.Fatalf("concurrency exceeded: %d", g.maxActive)
	}
}

func TestPendingFollowerStageKeepsWaiting(t *testing.T) {
	follower := azuredevops.ReleaseEnvironmentStatus{Name: "QA", Status: "notStarted"}
	follower.Conditions = append(follower.Conditions, struct {
		Name          string      `json:"name"`
		ConditionType interface{} `json:"conditionType"`
	}{"Dev", "environmentState"})
	manual := azuredevops.ReleaseEnvironmentStatus{Name: "Prod", Status: "notStarted"}
	dev := azuredevops.ReleaseEnvironmentStatus{Name: "Dev", Status: "succeeded"}

	if _, done := deploymentOutcome(azuredevops.Release{Environments: []azuredevops.ReleaseEnvironmentStatus{dev, follower}}); done {
		t.Fatal("a stage waiting for a succeeded predecessor must keep the release open")
	}
	if outcome, done := deploymentOutcome(azuredevops.Release{Environments: []azuredevops.ReleaseEnvironmentStatus{dev, manual}}); !done || outcome != "succeeded" {
		t.Fatalf("manual stages must not block: %s %v", outcome, done)
	}
	rejected := azuredevops.ReleaseEnvironmentStatus{Name: "Dev", Status: "rejected"}
	if outcome, done := deploymentOutcome(azuredevops.Release{Environments: []azuredevops.ReleaseEnvironmentStatus{rejected, follower}}); !done || outcome != "failed" {
		t.Fatalf("failure must end the wait: %s %v", outcome, done)
	}
}

func TestLastRetryChecksForCreatedRelease(t *testing.T) {
	f := newFake()
	f.createErr[1] = []error{errCreatedButFailed, errCreatedButFailed, errCreatedButFailed}
	o := opts()
	o.retries = 0 // the only attempt fails after the server created the release
	if r := deployOne(f, defs(1)[0], "P", 1, o, noSleep); r.status != "created" {
		t.Fatalf("status %s", r.status)
	}
}
