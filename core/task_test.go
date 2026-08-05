package core_test

import (
	"slices"
	"testing"
	"time"

	"github.com/cburgosro9303/atrio/core"
)

func humanActor(name string) core.Actor {
	return core.Actor{Kind: core.ActorKindHuman, Name: name}
}

var fixedTime = time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

func TestInitialState(t *testing.T) {
	t.Parallel()

	if got := core.InitialState(core.TaskTypeTask); got != core.TaskStateDraft {
		t.Errorf("InitialState(task) = %q, want %q", got, core.TaskStateDraft)
	}
	if got := core.InitialState(core.TaskTypeBug); got != core.TaskStateTriage {
		t.Errorf("InitialState(bug) = %q, want %q", got, core.TaskStateTriage)
	}
}

func TestNewTask(t *testing.T) {
	t.Parallel()

	by := humanActor("cesar")

	task := core.NewTask(core.TaskTypeTask, by, fixedTime)
	if task.State != core.TaskStateDraft {
		t.Fatalf("a new task starts in %q, got %q", core.TaskStateDraft, task.State)
	}
	if len(task.StateHistory) != 1 {
		t.Fatalf("a new task has exactly one history entry, got %d", len(task.StateHistory))
	}
	entry := task.StateHistory[0]
	if entry.From != "" {
		t.Errorf("the birth entry has no previous state, got %q", entry.From)
	}
	if entry.To != core.TaskStateDraft || entry.By != by || !entry.At.Equal(fixedTime) {
		t.Errorf("birth entry = %+v, want To=%q By=%+v At=%v", entry, core.TaskStateDraft, by, fixedTime)
	}

	bug := core.NewTask(core.TaskTypeBug, by, fixedTime)
	if bug.State != core.TaskStateTriage {
		t.Fatalf("a new bug starts in %q, got %q", core.TaskStateTriage, bug.State)
	}
}

func TestTaskTypeValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		typ  core.TaskType
		want bool
	}{
		{core.TaskTypeTask, true},
		{core.TaskTypeBug, true},
		{"widget", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run(string(tc.typ), func(t *testing.T) {
			t.Parallel()

			if got := tc.typ.Valid(); got != tc.want {
				t.Errorf("TaskType(%q).Valid() = %v, want %v", tc.typ, got, tc.want)
			}
		})
	}
}

func TestValidateBirth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		typ     core.TaskType
		state   core.TaskState
		wantErr bool
	}{
		{"task born in draft is legal", core.TaskTypeTask, core.TaskStateDraft, false},
		{"bug born in triage is legal", core.TaskTypeBug, core.TaskStateTriage, false},
		{"bug born outside triage is illegal", core.TaskTypeBug, core.TaskStateInProgress, true},
		{"bug born in draft is illegal", core.TaskTypeBug, core.TaskStateDraft, true},
		{"task born in triage is illegal", core.TaskTypeTask, core.TaskStateTriage, true},
		{"an unrecognized task type is illegal regardless of state", core.TaskType("widget"), core.TaskStateDraft, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := core.ValidateBirth(tc.typ, tc.state)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateBirth(%q, %q) error = %v, wantErr %v", tc.typ, tc.state, err, tc.wantErr)
			}
		})
	}
}

// allTaskStates lists every state in the enum, written independently of
// production code. TestCanTransition_FullStateGraph pins the edges between
// these states against expectedTaskEdges: an edge between two states already
// listed here that gets added, removed or widened in taskTransitions without
// a matching change to expectedTaskEdges fails the test. What it cannot
// catch: a new TaskState const added to the enum and never added to this
// list. Go gives no reflection over consts, so that specific gap is a
// code-review discipline, not a test guarantee — the same kind of documented
// blind spot CLAUDE.md records for internal/archtest's use of go list.
var allTaskStates = []core.TaskState{
	core.TaskStateDraft,
	core.TaskStateReadyForDev,
	core.TaskStateTriage,
	core.TaskStateInProgress,
	core.TaskStateBlocked,
	core.TaskStateInReview,
	core.TaskStateDeploying,
	core.TaskStateTesting,
	core.TaskStateCompleted,
	core.TaskStateCancelled,
}

// expectedTaskEdges pins the complete legal state graph as a literal,
// independently-written table — not by calling into CanTransition — so that
// widening or narrowing the production graph without a matching test change
// is caught rather than silently accepted (CLAUDE.md: "sincronizado por un
// test, no por un comentario", the same discipline archtest and citest use
// elsewhere in this repo). It excludes triage, which TestCanTransition_Triage
// covers on its own because its legality also depends on task type. The two
// edges into completed carry a further, environment-dependent condition that
// edgeExpected applies on top of this table, mirroring CanTransition itself.
var expectedTaskEdges = map[core.TaskState][]core.TaskState{
	core.TaskStateDraft:       {core.TaskStateReadyForDev, core.TaskStateCancelled},
	core.TaskStateReadyForDev: {core.TaskStateInProgress, core.TaskStateCancelled},
	core.TaskStateInProgress:  {core.TaskStateBlocked, core.TaskStateInReview, core.TaskStateCancelled},
	core.TaskStateBlocked:     {core.TaskStateInProgress, core.TaskStateCancelled},
	core.TaskStateInReview: {
		core.TaskStateInProgress, core.TaskStateBlocked, core.TaskStateDeploying,
		core.TaskStateCompleted, core.TaskStateCancelled,
	},
	core.TaskStateDeploying: {core.TaskStateTesting, core.TaskStateCancelled},
	core.TaskStateTesting: {
		core.TaskStateDeploying, core.TaskStateInProgress, core.TaskStateCompleted, core.TaskStateCancelled,
	},
	core.TaskStateCompleted: {},
	core.TaskStateCancelled: {},
}

// envCase is one combination of closure/current environment values the
// cross-product test exercises against every (type, from, to) triple. Only
// the two completed edges actually read these fields; running every state
// pair through all three cases also demonstrates that the rest of the graph
// is invariant to them.
type envCase struct {
	name               string
	closureEnvironment string
	currentEnvironment string
}

var envCases = []envCase{
	{"no closure environment declared", "", ""},
	{"closure environment declared, current matches it", "prod", "prod"},
	{"closure environment declared, current is a different one", "prod", "dev"},
}

// edgeExpected mirrors CanTransition's decision, written independently: a
// structural edge lookup in expectedTaskEdges, then the same two
// environment-dependent refinements CanTransition applies for the edges into
// completed.
func edgeExpected(from, to core.TaskState, closureEnvironment, currentEnvironment string) bool {
	base := false
	for _, candidate := range expectedTaskEdges[from] {
		if candidate == to {
			base = true
			break
		}
	}
	if !base {
		return false
	}

	switch {
	case from == core.TaskStateInReview && to == core.TaskStateCompleted:
		return closureEnvironment == ""
	case from == core.TaskStateTesting && to == core.TaskStateCompleted:
		return closureEnvironment != "" && currentEnvironment == closureEnvironment
	default:
		return true
	}
}

// TestCanTransition_FullStateGraph checks every (type, from, to,
// environment-case) combination — 2 types x 9 non-triage states x 9
// non-triage states x 3 environment cases — against the independently
// pinned expectations above. This both walks every legal transition and
// rejects every illegal one, which is a stronger guarantee than sampling
// either set by hand.
func TestCanTransition_FullStateGraph(t *testing.T) {
	t.Parallel()

	for _, typ := range []core.TaskType{core.TaskTypeTask, core.TaskTypeBug} {
		for _, from := range allTaskStates {
			for _, to := range allTaskStates {
				if from == core.TaskStateTriage || to == core.TaskStateTriage {
					continue // Covered by TestCanTransition_Triage: legality there depends on type.
				}
				for _, ec := range envCases {
					task := core.Task{
						Type:               typ,
						State:              from,
						ClosureEnvironment: ec.closureEnvironment,
						CurrentEnvironment: ec.currentEnvironment,
					}
					want := edgeExpected(from, to, ec.closureEnvironment, ec.currentEnvironment)
					got := core.CanTransition(task, to)
					if got != want {
						t.Errorf(
							"CanTransition(type=%q from=%q to=%q %s) = %v, want %v",
							typ, from, to, ec.name, got, want,
						)
					}
				}
			}
		}
	}
}

// TestCanTransition_Triage covers every combination involving triage: it is
// a legal source only for a bug, and never a legal destination for either
// type, since it is a birth-only state (see InitialState/ValidateBirth).
func TestCanTransition_Triage(t *testing.T) {
	t.Parallel()

	triageDestinations := []core.TaskState{
		core.TaskStateReadyForDev, core.TaskStateInProgress, core.TaskStateCancelled,
	}

	for _, typ := range []core.TaskType{core.TaskTypeTask, core.TaskTypeBug} {
		for _, to := range allTaskStates {
			want := typ == core.TaskTypeBug && slices.Contains(triageDestinations, to)
			got := core.CanTransition(core.Task{Type: typ, State: core.TaskStateTriage}, to)
			if got != want {
				t.Errorf("CanTransition(%q, triage, %q) = %v, want %v", typ, to, got, want)
			}
		}

		for _, from := range allTaskStates {
			if core.CanTransition(core.Task{Type: typ, State: from}, core.TaskStateTriage) {
				t.Errorf("CanTransition(%q, %q, triage) = true, want false: triage is never a legal destination", typ, from)
			}
		}
	}
}

// TestCanTransition_UnknownType confirms the graph is fail-closed against a
// TaskType outside {task, bug}: it never silently behaves like a task,
// mirroring the posture ValidateDecisionUpdate takes toward an unrecognized
// DecisionStatus.
func TestCanTransition_UnknownType(t *testing.T) {
	t.Parallel()

	task := core.Task{Type: "widget", State: core.TaskStateDraft}
	if core.CanTransition(task, core.TaskStateReadyForDev) {
		t.Error("CanTransition() with an unrecognized task type should be false, want fail-closed")
	}
}

// TestCanTransition_ClosureGating exercises, by name, the four scenarios the
// closure rule has to resolve: closing straight from review is legal only
// without a declared closure environment, and closing from the loop is legal
// only when the environment under test is that closure environment.
// TestCanTransition_FullStateGraph already covers these as part of the
// cross-product; this makes them individually legible as the acceptance
// criteria they are.
func TestCanTransition_ClosureGating(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		task  core.Task
		to    core.TaskState
		legal bool
	}{
		{
			name:  "closing straight from review is legal without a declared closure environment",
			task:  core.Task{Type: core.TaskTypeTask, State: core.TaskStateInReview},
			to:    core.TaskStateCompleted,
			legal: true,
		},
		{
			name: "closing straight from review is illegal with a declared closure environment",
			task: core.Task{
				Type: core.TaskTypeTask, State: core.TaskStateInReview, ClosureEnvironment: "prod",
			},
			to:    core.TaskStateCompleted,
			legal: false,
		},
		{
			name: "closing from testing is legal on the closure environment",
			task: core.Task{
				Type: core.TaskTypeTask, State: core.TaskStateTesting,
				ClosureEnvironment: "prod", CurrentEnvironment: "prod",
			},
			to:    core.TaskStateCompleted,
			legal: true,
		},
		{
			name: "closing from testing is illegal on a different environment",
			task: core.Task{
				Type: core.TaskTypeTask, State: core.TaskStateTesting,
				ClosureEnvironment: "prod", CurrentEnvironment: "staging",
			},
			to:    core.TaskStateCompleted,
			legal: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := core.CanTransition(tc.task, tc.to); got != tc.legal {
				t.Errorf("CanTransition(%+v, %q) = %v, want %v", tc.task, tc.to, got, tc.legal)
			}
		})
	}
}

func TestApplyTransition_RecordsHistory(t *testing.T) {
	t.Parallel()

	by := humanActor("cesar")
	task := core.NewTask(core.TaskTypeTask, by, fixedTime)

	later := fixedTime.Add(time.Hour)
	updated, err := task.ApplyTransition(core.TaskStateReadyForDev, by, later, "scoped and estimated")
	if err != nil {
		t.Fatalf("ApplyTransition() error = %v, want nil", err)
	}

	if updated.State != core.TaskStateReadyForDev {
		t.Errorf("State = %q, want %q", updated.State, core.TaskStateReadyForDev)
	}
	if len(updated.StateHistory) != 2 {
		t.Fatalf("StateHistory has %d entries, want 2", len(updated.StateHistory))
	}
	entry := updated.StateHistory[1]
	if entry.From != core.TaskStateDraft || entry.To != core.TaskStateReadyForDev {
		t.Errorf("new entry = %+v, want From=%q To=%q", entry, core.TaskStateDraft, core.TaskStateReadyForDev)
	}
	if entry.Reason != "scoped and estimated" {
		t.Errorf("Reason = %q, want %q", entry.Reason, "scoped and estimated")
	}

	// The receiver is untouched: ApplyTransition returns a new value.
	if task.State != core.TaskStateDraft || len(task.StateHistory) != 1 {
		t.Errorf("original task mutated: State=%q len(StateHistory)=%d", task.State, len(task.StateHistory))
	}
}

func TestApplyTransition_RejectsIllegalMove(t *testing.T) {
	t.Parallel()

	by := humanActor("cesar")
	task := core.NewTask(core.TaskTypeTask, by, fixedTime)

	if _, err := task.ApplyTransition(core.TaskStateInProgress, by, fixedTime, ""); err == nil {
		t.Fatal("ApplyTransition() to a non-adjacent state should have failed")
	}
}

// TestApplyTransition_RejectsMutationOfDefinitiveState confirms that once a
// task reaches completed or cancelled, no further transition is accepted.
func TestApplyTransition_RejectsMutationOfDefinitiveState(t *testing.T) {
	t.Parallel()

	by := humanActor("cesar")

	for _, definitive := range []core.TaskState{core.TaskStateCompleted, core.TaskStateCancelled} {
		t.Run(string(definitive), func(t *testing.T) {
			t.Parallel()

			task := core.Task{Type: core.TaskTypeTask, State: definitive}
			if _, err := task.ApplyTransition(core.TaskStateInProgress, by, fixedTime, "reopen"); err == nil {
				t.Errorf("ApplyTransition() from definitive state %q should have failed", definitive)
			}
		})
	}
}

// TestApplyTransition_RequiresBlockerWhenBlocking confirms ApplyTransition
// enforces ValidateBlocked at construction time: a caller cannot land a task
// in blocked without having already named a blocker on the receiver.
func TestApplyTransition_RequiresBlockerWhenBlocking(t *testing.T) {
	t.Parallel()

	by := humanActor("cesar")
	task := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInProgress}

	if _, err := task.ApplyTransition(core.TaskStateBlocked, by, fixedTime, ""); err == nil {
		t.Fatal("ApplyTransition() to blocked without a blocker should have failed")
	}

	task.BlockedBy = []core.Blocker{{Kind: core.BlockerKindUserQuestion, Note: "which environment ships first?"}}
	blocked, err := task.ApplyTransition(core.TaskStateBlocked, by, fixedTime, "")
	if err != nil {
		t.Fatalf("ApplyTransition() to blocked with a blocker present, error = %v, want nil", err)
	}
	if blocked.State != core.TaskStateBlocked {
		t.Errorf("State = %q, want %q", blocked.State, core.TaskStateBlocked)
	}
}

// TestEnterEnvironment covers EnterEnvironment on its own: it requires a
// named environment, sets CurrentEnvironment as part of entering deploying
// rather than leaving that to a separate assignment, and still defers to
// ApplyTransition/CanTransition for whether entering deploying is legal at
// all from the task's current state.
func TestEnterEnvironment(t *testing.T) {
	t.Parallel()

	by := humanActor("cesar")

	t.Run("an empty environment is rejected", func(t *testing.T) {
		t.Parallel()

		task := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInReview}
		if _, err := task.EnterEnvironment("", by, fixedTime, ""); err == nil {
			t.Error("EnterEnvironment(\"\") should have failed")
		}
	})

	t.Run("a named environment moves the task into deploying with it set", func(t *testing.T) {
		t.Parallel()

		task := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInReview}
		next, err := task.EnterEnvironment("dev", by, fixedTime, "")
		if err != nil {
			t.Fatalf("EnterEnvironment() error = %v, want nil", err)
		}
		if next.State != core.TaskStateDeploying {
			t.Errorf("State = %q, want %q", next.State, core.TaskStateDeploying)
		}
		if next.CurrentEnvironment != "dev" {
			t.Errorf("CurrentEnvironment = %q, want %q", next.CurrentEnvironment, "dev")
		}
	})

	t.Run("it still defers to the state graph for whether entering deploying is legal", func(t *testing.T) {
		t.Parallel()

		task := core.Task{Type: core.TaskTypeTask, State: core.TaskStateDraft}
		if _, err := task.EnterEnvironment("dev", by, fixedTime, ""); err == nil {
			t.Error("EnterEnvironment() from draft should have failed: deploying is not reachable from there")
		}
	})
}

// TestEnvironmentLoop_FullWalk walks the complete per-environment loop across
// three declared environments, in order: in_review opens the loop, and each
// round enters deploying for one environment through EnterEnvironment — the
// domain operation, not a direct field assignment — then tests it. It closes
// by checking that the task's ClosureEnvironment is one of the environments
// the loop actually produced, tying the loop to the standalone membership
// rule this package also owns (ValidateClosureEnvironment), and that
// completing from testing on that last environment is exactly the gated edge
// CanTransition allows.
func TestEnvironmentLoop_FullWalk(t *testing.T) {
	t.Parallel()

	by := humanActor("cesar")
	environments := []string{"dev", "staging", "prod"}

	task := core.NewTask(core.TaskTypeTask, by, fixedTime)
	task.ClosureEnvironment = "prod"

	var err error
	step := 0
	apply := func(to core.TaskState) {
		t.Helper()
		task, err = task.ApplyTransition(to, by, fixedTime.Add(time.Duration(step)*time.Minute), "")
		if err != nil {
			t.Fatalf("step %d: ApplyTransition(%q) error = %v", step, to, err)
		}
		step++
	}

	apply(core.TaskStateReadyForDev)
	apply(core.TaskStateInProgress)
	apply(core.TaskStateInReview)

	var walked []string
	for _, env := range environments {
		t.Helper()
		task, err = task.EnterEnvironment(env, by, fixedTime.Add(time.Duration(step)*time.Minute), "")
		if err != nil {
			t.Fatalf("EnterEnvironment(%q) error = %v", env, err)
		}
		step++
		walked = append(walked, task.CurrentEnvironment)
		apply(core.TaskStateTesting)
	}
	apply(core.TaskStateCompleted)

	if !task.State.IsDefinitive() {
		t.Fatalf("task should have finished in a definitive state, got %q", task.State)
	}
	if !slices.Equal(walked, environments) {
		t.Errorf("environments walked = %v, want %v in order", walked, environments)
	}
	// Validated against walked — what the loop actually produced — and not
	// against the environments literal above: this is what the doc comment
	// promises ("the environments the loop actually produced"), and walked
	// is asserted equal to that literal just above, so this exercises the
	// loop's own output, not the test's input fixture.
	if err := core.ValidateClosureEnvironment(task.ClosureEnvironment, walked); err != nil {
		t.Errorf("ValidateClosureEnvironment(%q, %v) error = %v, want nil", task.ClosureEnvironment, walked, err)
	}

	wantHistoryLen := 1 + step // +1 for the birth entry.
	if len(task.StateHistory) != wantHistoryLen {
		t.Errorf("StateHistory has %d entries, want %d", len(task.StateHistory), wantHistoryLen)
	}
}

func TestValidateBlocked(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		task    core.Task
		wantErr bool
	}{
		{"a non-blocked task needs no blocker", core.Task{State: core.TaskStateInProgress}, false},
		{
			"a blocked task with a blocker is consistent",
			core.Task{
				State:     core.TaskStateBlocked,
				BlockedBy: []core.Blocker{{Kind: core.BlockerKindTask, Ref: "01ARZ3NDEKTSV4RRFFQ69G5FAV"}},
			},
			false,
		},
		{"a blocked task without a blocker is rejected", core.Task{State: core.TaskStateBlocked}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := core.ValidateBlocked(tc.task)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateBlocked(%+v) error = %v, wantErr %v", tc.task, err, tc.wantErr)
			}
		})
	}
}

func TestValidateTaskUpdate(t *testing.T) {
	t.Parallel()

	by := humanActor("cesar")
	definitive := core.Task{
		Type:  core.TaskTypeTask,
		State: core.TaskStateCompleted,
		StateHistory: []core.StateTransition{
			{To: core.TaskStateDraft, By: by, At: fixedTime},
			{From: core.TaskStateTesting, To: core.TaskStateCompleted, By: by, At: fixedTime},
		},
	}

	t.Run("identical copy is legal", func(t *testing.T) {
		t.Parallel()

		copyOfTask := definitive
		if err := core.ValidateTaskUpdate(definitive, copyOfTask); err != nil {
			t.Errorf("ValidateTaskUpdate() error = %v, want nil", err)
		}
	})

	t.Run("any field change is rejected once definitive", func(t *testing.T) {
		t.Parallel()

		mutated := definitive
		mutated.ClosureEnvironment = "prod"
		if err := core.ValidateTaskUpdate(definitive, mutated); err == nil {
			t.Error("ValidateTaskUpdate() should reject a change to a definitive task")
		}
	})

	t.Run("a non-definitive task accepts a field change at the same state", func(t *testing.T) {
		t.Parallel()

		before := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInProgress}
		after := before
		after.ClosureEnvironment = "prod"
		if err := core.ValidateTaskUpdate(before, after); err != nil {
			t.Errorf("ValidateTaskUpdate() error = %v, want nil for a non-definitive task", err)
		}
	})

	t.Run("a legal state change between two versions is accepted", func(t *testing.T) {
		t.Parallel()

		before := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInProgress}
		after := before
		after.State = core.TaskStateInReview
		if err := core.ValidateTaskUpdate(before, after); err != nil {
			t.Errorf("ValidateTaskUpdate() error = %v, want nil for a legal transition", err)
		}
	})

	t.Run("an illegal state change between two versions is rejected even outside ApplyTransition", func(t *testing.T) {
		t.Parallel()

		before := core.Task{Type: core.TaskTypeTask, State: core.TaskStateDraft}
		after := before
		after.State = core.TaskStateDeploying // skips ready_for_dev, in_progress and in_review.
		if err := core.ValidateTaskUpdate(before, after); err == nil {
			t.Error("ValidateTaskUpdate() should reject a state change that is not a legal edge of the graph")
		}
	})

	// The four cases below are the regression the coordinator's review
	// reproduced empirically: a single write can change State and the
	// environment fields together, so checking either version's fields
	// alone lets an illegal closure through from one direction while
	// blocking it from the other. Both directions have to be rejected, and
	// the two legitimate closures — one straight from review, one from the
	// loop — have to keep working once both checks are in place.

	t.Run("adding a closure environment in the same write that completes from review is rejected", func(t *testing.T) {
		t.Parallel()

		before := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInReview, ClosureEnvironment: ""}
		after := before
		after.State = core.TaskStateCompleted
		after.ClosureEnvironment = "prod" // Never entered the loop for it.
		if err := core.ValidateTaskUpdate(before, after); err == nil {
			t.Error("ValidateTaskUpdate() should reject completing from review while adding a closure environment in the same write")
		}
	})

	t.Run("removing the closure environment in the same write that completes from review is rejected", func(t *testing.T) {
		t.Parallel()

		before := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInReview, ClosureEnvironment: "prod"}
		after := before
		after.State = core.TaskStateCompleted
		after.ClosureEnvironment = "" // Erasing it does not un-declare the loop that was owed.
		if err := core.ValidateTaskUpdate(before, after); err == nil {
			t.Error("ValidateTaskUpdate() should reject completing from review while erasing a declared closure environment in the same write")
		}
	})

	t.Run("changing the closure environment in the same write that completes from testing is rejected", func(t *testing.T) {
		t.Parallel()

		before := core.Task{
			Type: core.TaskTypeTask, State: core.TaskStateTesting,
			ClosureEnvironment: "prod", CurrentEnvironment: "prod",
		}
		after := before
		after.State = core.TaskStateCompleted
		after.ClosureEnvironment = "staging" // staging was never tested.
		if err := core.ValidateTaskUpdate(before, after); err == nil {
			t.Error("ValidateTaskUpdate() should reject completing from testing when the closure environment changes to one never tested")
		}
	})

	t.Run("closing straight from review without a closure environment on either version is accepted", func(t *testing.T) {
		t.Parallel()

		before := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInReview, ClosureEnvironment: ""}
		after := before
		after.State = core.TaskStateCompleted
		if err := core.ValidateTaskUpdate(before, after); err != nil {
			t.Errorf("ValidateTaskUpdate() error = %v, want nil for a legitimate direct close", err)
		}
	})

	t.Run("closing from testing with matching environments on both versions is accepted", func(t *testing.T) {
		t.Parallel()

		before := core.Task{
			Type: core.TaskTypeTask, State: core.TaskStateTesting,
			ClosureEnvironment: "prod", CurrentEnvironment: "prod",
		}
		after := before
		after.State = core.TaskStateCompleted
		if err := core.ValidateTaskUpdate(before, after); err != nil {
			t.Errorf("ValidateTaskUpdate() error = %v, want nil for a legitimate loop close", err)
		}
	})

	t.Run("changing the closure environment mid-task without completing stays legal", func(t *testing.T) {
		t.Parallel()

		before := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInProgress, ClosureEnvironment: "dev"}
		after := before
		after.State = core.TaskStateInReview
		after.ClosureEnvironment = "staging"
		if err := core.ValidateTaskUpdate(before, after); err != nil {
			t.Errorf("ValidateTaskUpdate() error = %v, want nil: the gate only covers edges into completed", err)
		}
	})

	t.Run("moving to blocked without naming a blocker is rejected", func(t *testing.T) {
		t.Parallel()

		before := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInProgress}
		after := before
		after.State = core.TaskStateBlocked
		if err := core.ValidateTaskUpdate(before, after); err == nil {
			t.Error("ValidateTaskUpdate() should reject moving to blocked without a blocker")
		}
	})

	t.Run("moving to blocked while naming a blocker is accepted", func(t *testing.T) {
		t.Parallel()

		before := core.Task{Type: core.TaskTypeTask, State: core.TaskStateInProgress}
		after := before
		after.State = core.TaskStateBlocked
		after.BlockedBy = []core.Blocker{{Kind: core.BlockerKindPendingDecision, Ref: "01ARZ3NDEKTSV4RRFFQ69G5FAV"}}
		if err := core.ValidateTaskUpdate(before, after); err != nil {
			t.Errorf("ValidateTaskUpdate() error = %v, want nil", err)
		}
	})

	t.Run("a blocked task cannot lose its only blocker while staying blocked", func(t *testing.T) {
		t.Parallel()

		blocked := core.Task{
			Type:      core.TaskTypeTask,
			State:     core.TaskStateBlocked,
			BlockedBy: []core.Blocker{{Kind: core.BlockerKindTask, Ref: "01ARZ3NDEKTSV4RRFFQ69G5FAV"}},
		}
		stripped := blocked
		stripped.BlockedBy = nil
		if err := core.ValidateTaskUpdate(blocked, stripped); err == nil {
			t.Error("ValidateTaskUpdate() should reject a blocked task losing its only blocker")
		}
	})
}
