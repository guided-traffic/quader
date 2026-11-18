//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// podWatch records every change of the quader pods from `kubectl get pods -w`, stamped with the
// time it arrived: the time a pod spent Pending, and every container that was terminated by
// SIGKILL (exit code 137) or by the OOM killer. A watch sees each transition; polling would miss
// the final state of a pod that is deleted a moment later.
type podWatch struct {
	cmd     *exec.Cmd
	mu      sync.Mutex
	pending map[string]time.Time // pod -> first seen Pending
	pendFor map[string]time.Duration
	killed  []string
	seen    map[string]bool
	done    chan struct{}
	raw     *os.File
	// firstDeleted is when the first pod got a deletion timestamp: the first eviction of a drain.
	firstDeleted time.Time
	// deleted holds every pod that got a deletion timestamp, with the time the watch saw it first: the evicted pods.
	deleted map[string]time.Time
}

func startPodWatch(t testing.TB, selector string) *podWatch {
	since := time.Now()
	w := &podWatch{pending: map[string]time.Time{}, pendFor: map[string]time.Duration{}, seen: map[string]bool{}, deleted: map[string]time.Time{}, done: make(chan struct{})}
	var err error
	if w.raw, err = os.Create(filepath.Join(cfg.out, "drain-pod-watch.jsonl")); err != nil {
		t.Fatal(err)
	}
	w.cmd = exec.Command("kubectl", "--context", cfg.kubeContext, "-n", cfg.namespace, "get", "pods", "-l", selector, "-w", "-o", "json", "--output-watch-events")
	out, err := w.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(w.done)
		dec := json.NewDecoder(out)
		for dec.More() {
			var ev struct {
				Type   string
				Object struct {
					Metadata struct {
						Name              string
						DeletionTimestamp *time.Time
					}
					Status struct {
						Phase             string
						ContainerStatuses []struct {
							Name  string
							State struct {
								Terminated *struct {
									ExitCode   int
									Reason     string
									FinishedAt time.Time
								}
							}
							LastState struct {
								Terminated *struct {
									ExitCode   int
									Reason     string
									FinishedAt time.Time
								}
							}
						}
					}
				}
			}
			if err := dec.Decode(&ev); err != nil {
				return
			}
			now := time.Now()
			b, _ := json.Marshal(map[string]any{"at": now, "type": ev.Type, "pod": ev.Object.Metadata.Name, "phase": ev.Object.Status.Phase})
			fmt.Fprintln(w.raw, string(b))
			name := ev.Object.Metadata.Name
			w.mu.Lock()
			w.seen[name] = true
			if ev.Object.Metadata.DeletionTimestamp != nil {
				if w.firstDeleted.IsZero() {
					w.firstDeleted = now
				}
				if _, ok := w.deleted[name]; !ok {
					w.deleted[name] = now
				}
			}
			switch ev.Object.Status.Phase {
			case "Pending":
				if _, ok := w.pending[name]; !ok {
					w.pending[name] = now
				}
			default:
				if at, ok := w.pending[name]; ok {
					w.pendFor[name] = now.Sub(at)
					delete(w.pending, name)
				}
			}
			for _, c := range ev.Object.Status.ContainerStatuses {
				for _, term := range []*struct {
					ExitCode   int
					Reason     string
					FinishedAt time.Time
				}{c.State.Terminated, c.LastState.Terminated} {
					// Only terminations since the watch started: an earlier kill stays in lastState.
					if term != nil && term.FinishedAt.After(since) && (term.ExitCode == 137 || term.Reason == "OOMKilled") {
						w.killed = append(w.killed, fmt.Sprintf("%s/%s exit %d %s", name, c.Name, term.ExitCode, term.Reason))
					}
				}
			}
			w.mu.Unlock()
		}
	}()
	t.Cleanup(func() { w.stop() })
	return w
}

func (w *podWatch) stop() {
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
	<-w.done
	_ = w.cmd.Wait()
	_ = w.raw.Close()
}

// longestPending is the longest time a pod was Pending, counting a pod that is still Pending now.
func (w *podWatch) longestPending() (string, time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var name string
	var longest time.Duration
	for n, d := range w.pendFor {
		if d > longest {
			name, longest = n, d
		}
	}
	for n, at := range w.pending {
		if d := time.Since(at); d > longest {
			name, longest = n, d
		}
	}
	return name, longest
}

// nodesHosting returns the nodes that host a live pod of every selector.
func nodesHosting(t testing.TB, selectors ...string) map[string]bool {
	t.Helper()
	var out map[string]bool
	for _, sel := range selectors {
		on := map[string]bool{}
		for _, p := range livePods(t, sel) {
			on[p.Node] = true
		}
		if out == nil {
			out = on
			continue
		}
		for n := range out {
			if !on[n] {
				delete(out, n)
			}
		}
	}
	return out
}

// drainNode picks the node of variant A (R-HA-059): it hosts an api, a packages and a main worker pod (not one of a
// pool, whose queues exclude the probe jobs) and is not the PostgreSQL primary's node: CloudNativePG switches the primary
// over as soon as its node is cordoned, which ends every connection and every job lease (Test and Release run
// 42406831768). When the only node that hosts all three is the primary's, the primary is switched over first (variant B);
// the standby that then stays on the node is not evicted, because drain.sh evicts only the release's pods.
func drainNode(t *testing.T) string {
	t.Helper()
	for attempt := 0; attempt < 2; attempt++ {
		primary := strings.TrimSpace(kn(t, "get", "pod", pgPrimary(t), "-o", "jsonpath={.spec.nodeName}"))
		var both []string
		for n := range nodesHosting(t, selAPI, selPackages, selMainWorker) {
			if n != primary {
				both = append(both, n)
			}
		}
		sort.Strings(both)
		if len(both) > 0 {
			return both[0]
		}
		t.Logf("the only node with an api, a packages and a worker pod is the PostgreSQL primary's (%s); switching the primary over first", primary)
		script(t, "failover.sh")
	}
	t.Fatal("no node hosts an api, a packages and a worker pod without the PostgreSQL primary")
	return ""
}

type probeJob struct {
	State        string          `json:"state"`
	Attempt      int             `json:"attempt"`
	MaxAttempts  int             `json:"max_attempts"`
	ProgressDone int             `json:"progress_done"`
	Checkpoint   json.RawMessage `json:"checkpoint"`
	Result       struct {
		Steps   int      `json:"steps"`
		Hops    []string `json:"hops"`
		Resumes []int    `json:"resumes"`
	} `json:"result"`
}

func TestE14Drain(t *testing.T) {
	t.Run("R-ARCH-024 R-HA-011 R-HA-059 E0.14 E1.13 draining a node with an api, a packages and a worker pod under load, an upload and download loop on generic-local included: only 200, no failed transfer, the jobs resume on the other worker from their cursor, no SIGKILL, nothing Pending for 60 s", func(t *testing.T) {
		const steps = 150
		node := drainNode(t)
		var onNode []pod
		for _, p := range livePods(t, selQuader) {
			if p.Node == node {
				onNode = append(onNode, p)
			}
		}
		t.Logf("draining %s, which hosts %v", node, podNames(onNode))
		mainWorkers := map[string]bool{}
		for _, p := range livePods(t, selMainWorker) {
			mainWorkers[p.Name] = true
		}
		var drainedWorker string
		for _, p := range onNode {
			if mainWorkers[p.Name] {
				drainedWorker = p.Name
			}
		}
		if drainedWorker == "" {
			t.Fatal("the chosen node has no main worker pod")
		}
		// The shutdown of the drained pods is logged by the pods themselves; the logs are streamed so they outlive the pods,
		// and the requests in them after the readiness flip measure the edge's endpoint-removal time (drain_measure_test.go).
		streams := streamPodLogs(t, "drain-", onNode)

		watch := startPodWatch(t, selQuader)
		drv := startDriverLoad(t, "drain", 20*time.Minute)
		k := startK6(t, "drain", 30*time.Minute)
		// E1.13: an upload and download loop on generic-local runs through the drain as well (transfer.js: PUT, GET,
		// SHA-256 compared), with the token of a service account that may write there.
		kx := startK6Script(t, "drain-xfer", "transfer.js", 30*time.Minute, k6Opts{rate: 4, prefix: "T ", token: transferToken(t)})

		// Probe jobs: enough that both workers carry some, so the drained one certainly runs a job.
		var ids []string
		for i := 0; i < 6; i++ {
			ids = append(ids, strings.TrimSpace(driver(t, "probe", "-steps", fmt.Sprint(steps), "-interval-ms", "1000")))
		}
		waitFor(t, "both workers to run a probe job", 60*time.Second, time.Second, func() bool {
			rows := sql(t, `SELECT r.name FROM quader.job j JOIN quader.replica r ON r.instance_uid = j.locked_by WHERE j.kind = 'system.probe' AND j.state = 'running' AND j.progress_total = 150 GROUP BY 1`)
			return len(strings.Fields(rows)) >= 2
		})
		owners := sql(t, `SELECT r.name || '=' || count(*) FROM quader.job j JOIN quader.replica r ON r.instance_uid = j.locked_by WHERE j.kind = 'system.probe' AND j.state = 'running' GROUP BY r.name`)
		t.Logf("running probe jobs per worker before the drain: %s", strings.ReplaceAll(owners, "\n", " "))
		time.Sleep(5 * time.Second)

		// The drain is judged by the rows of the processes it stops, by instance UID: a pod whose container an earlier test killed
		// and the kubelet restarted in place keeps its name, and the killed process's row stays listed as lost under that name
		// for 24 h (F-146: TestE06Lease's SIGKILL victim was the worker this drain evicted).
		lostBefore := lostReplicas(t)
		drainedRows := liveRowsOf(t, onNode)
		drainStart := time.Now()
		t.Logf("drain starts %s", drainStart.Format("15:04:05"))
		script(t, "drain.sh", node) // the drain alone: drain, wait for the rollouts, uncordon
		drainTook := time.Since(drainStart)
		t.Logf("drain.sh returned after %s", drainTook.Round(time.Second))

		// The jobs finish.
		finished := map[string]probeJob{}
		waitFor(t, "every probe job to end", 6*time.Minute, 5*time.Second, func() bool {
			for _, id := range ids {
				if _, ok := finished[id]; ok {
					continue
				}
				var j probeJob
				if err := jsonUnmarshal(driver(t, "job", id), &j); err != nil {
					t.Fatal(err)
				}
				if j.State == "succeeded" || j.State == "failed" || j.State == "cancelled" {
					finished[id] = j
				}
			}
			return len(finished) == len(ids)
		})
		moved := 0
		for _, id := range ids {
			j := finished[id]
			t.Logf("job %s: %s attempt %d/%d steps %d hops %d resumed at %v", id, j.State, j.Attempt, j.MaxAttempts, j.Result.Steps, len(j.Result.Hops), j.Result.Resumes)
			if j.State != "succeeded" || j.Result.Steps != steps {
				t.Errorf("job %s ended %s with %d steps, want succeeded with %d", id, j.State, j.Result.Steps, steps)
			}
			if j.Attempt != 1 {
				t.Errorf("job %s: attempt %d, want 1 (a release at shutdown does not count an attempt)", id, j.Attempt)
			}
			if len(j.Result.Hops) > 1 {
				moved++
				if len(j.Result.Resumes) == 0 || j.Result.Resumes[0] <= 0 || j.Result.Resumes[0] >= steps {
					t.Errorf("job %s moved but did not resume from a cursor inside the run: %v", id, j.Result.Resumes)
				}
			}
		}
		if moved == 0 {
			t.Errorf("no probe job moved to the other worker: the drained worker %s did not release any job", drainedWorker)
		}
		t.Logf("%d of %d probe jobs moved to the other worker and finished from their cursor", moved, len(ids))

		time.Sleep(10 * time.Second)
		watch.stop()
		driver(t, "stop")
		ds := drv.wait(t)
		res := k.stop(t)
		xres := kx.stopTransfer(t)
		noteResult(t, "e14-drain", map[string]any{"node": node, "drain_seconds": drainTook.Seconds(), "k6_status": res.Status, "driver": ds, "pending_pods": watch.pendFor, "moved_jobs": moved, "transfers": xres.Status})

		res.only200(t)
		if len(xres.Failures) != 0 || xres.Bad != 0 || xres.Dropped != 0 {
			first := xres.Failures
			if len(first) > 5 {
				first = first[:5]
			}
			// what the limiters decided on each replica, for the one who reads a refusal (the k6 log has the headers)
			for _, p := range readyServingPods(t) {
				for k, v := range metricsOf(t, p) {
					if strings.HasPrefix(k, "quader_ratelimit_decisions_total{limiter=\"api_write\"") || strings.HasPrefix(k, "quader_ratelimit_fallback{limiter=\"api_write\"") || strings.HasPrefix(k, "quader_api_rate_limited_total") {
						t.Logf("%s %s = %v", p, k, v)
					}
				}
			}
			t.Errorf("E1.13 the upload and download loop on generic-local failed during the drain: %d failures, %d wrong checksums, %v dropped iterations; first: %v", len(xres.Failures), xres.Bad, xres.Dropped, first)
		}
		if xres.Status["put 201"]+xres.Status["put 200"] < 100 || xres.Status["get 200"] < 100 {
			t.Errorf("E1.13 the transfer loop made too few requests to say anything: %v", xres.Status)
		}
		if unc := strings.TrimSpace(kn(t, "get", "node", node, "-o", "jsonpath={.spec.unschedulable}")); unc == "true" {
			t.Errorf("node %s is still cordoned", node)
		}
		if len(watch.killed) != 0 {
			t.Errorf("containers were killed: %v", watch.killed)
		}
		if name, d := watch.longestPending(); d > 60*time.Second {
			t.Errorf("quader pod %s was Pending for %s", name, d.Round(time.Second))
		} else {
			t.Logf("longest Pending of a quader pod: %s (%s)", d.Round(100*time.Millisecond), name)
		}
		checkEvents(t, onNode)
		checkRegistryAfterDrain(t, drainedRows, lostBefore, streams)
		checkEndpointRemoval(t, "e14-drain", streams)
		checkReleaseServes(t, 2, 3, 4)
	})
}

// checkRegistryAfterDrain is what the status screen makes of the registry once the drain is over (R-OPS-003, R-UI-038,
// Q-OPS-210): every drained process signed off, so its row is gone (a clean stop deletes it) or, if the deletion did not fit
// the close phase, left in the state draining, which the list does not show; no listed replica is lost; and every main
// role has a ready replica, so the header is not red. The drained processes are the rows in drained, by instance UID; rows
// already lost before the drain (lostBefore, by instance UID) are not the drain's, whatever pod name they carry.
//
// A row left in another state is the drain's failure, with one case told apart: the process's own log says that the database
// answered neither the mark draining nor the deletion during its whole shutdown (Q-HA-310). That row is listed as lost, a
// warning, and the product cannot avoid it from inside the process; the test names it instead of reporting a sign-off that
// failed although the database answered.
func checkRegistryAfterDrain(t *testing.T, drained map[string]string, lostBefore map[string]bool, streams []*podLogStream) {
	t.Helper()
	if len(drained) == 0 {
		t.Error("R-OPS-003 no live registry row of a drained pod was found before the drain")
		return
	}
	uids := make([]string, 0, len(drained))
	for uid := range drained {
		uids = append(uids, "'"+uid+"'")
	}
	sort.Strings(uids)
	if left := sql(t, `SELECT instance_uid || '=' || state FROM quader.replica WHERE instance_uid IN (`+strings.Join(uids, ",")+`)`); left != "" {
		rows := strings.Fields(left)
		for _, r := range rows {
			uid, state, _ := strings.Cut(r, "=")
			if state == "draining" {
				continue
			}
			name := drained[uid]
			if dbAwayForTheWholeShutdown(t, streams, name) {
				t.Errorf("R-OPS-003 Q-HA-310 the drained process %s (%s) left its row in the state %s because the database answered neither the mark draining nor the deletion during its whole shutdown (its log says so): the row is listed as lost; the drain test needs a database that answers",
					name, uid, state)
				continue
			}
			t.Errorf("R-OPS-003 the drained process %s (%s) left its row in the state %s although the database answered during its shutdown: it is listed as lost", name, uid, state)
		}
		t.Logf("rows of drained processes left behind (draining is signed off, not listed): %v", rows)
	}
	// A row left in the state draining is listed until system.heartbeat_stale_after has passed, so the list is read again
	// until it settles.
	waitFor(t, "R-OPS-003 R-UI-038 no drained process listed, no lost row, a ready replica of every main role", 60*time.Second, 2*time.Second, func() bool {
		reps, err := replicas()
		if err != nil {
			t.Logf("listing the replicas: %v", err)
			return false
		}
		ready := map[string]bool{}
		for _, r := range reps {
			if _, mine := drained[r.InstanceUID]; mine || (r.Lost && !lostBefore[r.InstanceUID]) {
				t.Logf("still listed: %s %s (state %s, lost %v)", r.Name, r.InstanceUID, r.State, r.Lost)
				return false
			}
			if r.State == "ready" {
				ready[r.role()] = true
			}
		}
		return ready["api"] && ready["packages"] && ready["worker"]
	})
}

// dbAwayForTheWholeShutdown reports whether the streamed log of the pod says that its registry row was neither marked draining
// nor deleted because the database did not answer (the line of Registry.Stop, Q-HA-307).
func dbAwayForTheWholeShutdown(t *testing.T, streams []*podLogStream, pod string) bool {
	t.Helper()
	for _, s := range streams {
		if s.Pod != pod {
			continue
		}
		s.finish(10 * time.Second) // the pod is gone by now; its stream ends by itself
		b, err := os.ReadFile(s.File)
		if err != nil {
			t.Logf("reading the log of %s: %v", pod, err)
			return false
		}
		return strings.Contains(string(b), "replica row neither marked draining nor deleted")
	}
	return false
}

// liveRowsOf returns the instance UID and name of the live registry row of each pod: the processes a drain of those pods stops.
// A pod restarted in place has older rows under its name, which are lost and not returned.
func liveRowsOf(t *testing.T, pods []pod) map[string]string {
	t.Helper()
	reps, err := replicas()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, p := range pods {
		names[p.Name] = true
	}
	out := map[string]string{}
	for _, r := range reps {
		if names[r.Name] && !r.Lost {
			out[r.InstanceUID] = r.Name
		}
	}
	return out
}

// lostReplicas names the instance UIDs of the replicas the registry lists as lost.
func lostReplicas(t *testing.T) map[string]bool {
	t.Helper()
	reps, err := replicas()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, r := range reps {
		if r.Lost {
			out[r.InstanceUID] = true
		}
	}
	return out
}

func podNames(ps []pod) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

// checkEvents reads the kubelet's events for the drained pods: a container that does not stop within
// the grace period is killed, which the kubelet records; none of that may appear.
func checkEvents(t *testing.T, drained []pod) {
	t.Helper()
	raw := kn(t, "get", "events", "-o", "json")
	_ = os.WriteFile(filepath.Join(cfg.out, "drain-events.json"), []byte(raw), 0o644)
	var l struct {
		Items []struct {
			Reason, Message, Type string
			InvolvedObject        struct{ Name string }
		}
	}
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		t.Fatal(err)
	}
	mine := map[string]bool{}
	for _, p := range drained {
		mine[p.Name] = true
	}
	killing := 0
	sc := bufio.NewScanner(strings.NewReader(""))
	_ = sc
	for _, e := range l.Items {
		if !mine[e.InvolvedObject.Name] {
			continue
		}
		low := strings.ToLower(e.Message)
		if e.Reason == "Killing" {
			killing++
		}
		if strings.Contains(low, "sigkill") || strings.Contains(low, "exit code 137") || strings.Contains(low, "grace period") && e.Reason != "Killing" || e.Reason == "FailedKillPod" || strings.Contains(e.Reason, "OOM") {
			t.Errorf("event %s on %s: %s", e.Reason, e.InvolvedObject.Name, e.Message)
		}
	}
	t.Logf("%d Killing events (the kubelet's SIGTERM) for the drained pods %v, none with SIGKILL", killing, podNames(drained))
}
