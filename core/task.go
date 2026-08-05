package core

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

// TaskType distinguishes a task from a bug. Both share the same entity and
// the same state enum; only their birth state and the presence of an origin
// reference differ (01-definicion-producto.md, section 8).
type TaskType string

const (
	TaskTypeTask TaskType = "task"
	TaskTypeBug  TaskType = "bug"
)

// Valid reports whether t is one of the two task types the domain
// recognizes. ValidateBirth and CanTransition are both fail-closed against a
// type that fails this check, the same posture ValidateDecisionUpdate takes
// against an unrecognized DecisionStatus: the JSON Schema enum is the real
// gate against a stored artifact, but a value built or passed in memory
// never gets to rely on that gate having run yet.
func (t TaskType) Valid() bool {
	return t == TaskTypeTask || t == TaskTypeBug
}

// TaskState is one point in the task/bug lifecycle. The enum spans the full
// lifecycle from construction to a definitive state from day one, even though
// M1 only exercises the early ones, so that reaching the later states costs
// no schema migration (task.schema.json).
type TaskState string

const (
	// Construction: a task is drafted and refined until it is ready for work.
	TaskStateDraft       TaskState = "draft"
	TaskStateReadyForDev TaskState = "ready_for_dev"

	// TaskStateTriage is where a bug, and only a bug, is born.
	TaskStateTriage TaskState = "triage"

	// Execution.
	TaskStateInProgress TaskState = "in_progress"
	TaskStateBlocked    TaskState = "blocked"
	TaskStateInReview   TaskState = "in_review"

	// The per-environment loop: deploying then testing, repeated for each
	// environment in the project's order, tracked by CurrentEnvironment
	// rather than by one state per environment.
	TaskStateDeploying TaskState = "deploying"
	TaskStateTesting   TaskState = "testing"

	// Definitive: no further transition is legal once reached.
	TaskStateCompleted TaskState = "completed"
	TaskStateCancelled TaskState = "cancelled"
)

// IsDefinitive reports whether a state is terminal. A task or bug in a
// definitive state rejects any further mutation (schemas/README.md: "Estados
// definitivos (completed, cancelled) inmutables").
func (s TaskState) IsDefinitive() bool {
	return s == TaskStateCompleted || s == TaskStateCancelled
}

// InitialState is the state a task/bug of the given type is born in: a task
// starts in draft, a bug in triage (01-definicion-producto.md, section 8).
// It is total over TaskType — an unrecognized type falls back to draft — so
// that ValidateBirth alone carries the "is this even a real type" check
// rather than splitting it across two functions.
func InitialState(taskType TaskType) TaskState {
	if taskType == TaskTypeBug {
		return TaskStateTriage
	}
	return TaskStateDraft
}

// ValidateBirth reports whether state is the legal birth state for taskType.
// A task is never born anywhere but draft, and a bug never anywhere but
// triage; NewTask always gets this right by construction, but the predicate
// is exposed so callers reconstructing or checking a task from stored data
// (or from a flow that mints a bug mid-run) can enforce the same invariant
// without going through I/O. It also rejects a taskType outside {task, bug}
// outright, rather than silently treating it as a task the way InitialState's
// fallback would.
func ValidateBirth(taskType TaskType, state TaskState) error {
	if !taskType.Valid() {
		return fmt.Errorf("core: %q is not a recognized task type", taskType)
	}
	if want := InitialState(taskType); state != want {
		return fmt.Errorf("core: a %q is born in state %q, not %q", taskType, want, state)
	}
	return nil
}

// BlockerKind is what a blocked task is waiting on.
type BlockerKind string

const (
	BlockerKindTask            BlockerKind = "task"
	BlockerKindUserQuestion    BlockerKind = "user_question"
	BlockerKindPendingDecision BlockerKind = "pending_decision"
)

// Blocker is one reason a task is blocked: another task, a question put to
// the user, or a decision still pending. ValidateBlocked is what gives this
// type teeth: a task in the blocked state must carry at least one.
type Blocker struct {
	Kind BlockerKind
	Ref  string // ULID of the referenced task or decision; empty for a bare question.
	Note string // The question asked, or free-form context.
}

// StateTransition is one recorded move in a task's own audit trail
// (stateHistory). From is the empty string on the entry created at birth,
// which has no previous state.
type StateTransition struct {
	From   TaskState
	To     TaskState
	By     Actor
	At     time.Time
	Reason string
}

// Task is a unit of work, or a bug, carried as a single entity across its
// whole lifecycle (01-definicion-producto.md, section 8). Only the fields
// that domain rules in this package act on are modeled here; mapping this
// entity to and from its stored JSON form belongs to store (T-020).
type Task struct {
	Type               TaskType
	State              TaskState
	StateHistory       []StateTransition
	BlockedBy          []Blocker
	ClosureEnvironment string
	CurrentEnvironment string // Set by EnterEnvironment; not meant to be assigned directly.
	OriginTaskRef      string // Set only for a bug born from a task; empty otherwise.
}

// NewTask creates a task or bug in its birth state, with the first entry of
// its own history recording who created it and when. The caller supplies the
// actor and the instant: core sources neither a clock nor an identity on its
// own.
func NewTask(taskType TaskType, by Actor, at time.Time) Task {
	state := InitialState(taskType)
	return Task{
		Type:  taskType,
		State: state,
		StateHistory: []StateTransition{
			{To: state, By: by, At: at},
		},
	}
}

// taskTransitions is the legal state graph shared by tasks and bugs alike,
// before the two environment-aware refinements CanTransition layers on top
// (see below). triage is deliberately absent as a source here: it is
// reachable only as a birth state (see InitialState/ValidateBirth), never as
// an intermediate one, so no entry lists it as a destination either.
//
// The order: construction (draft -> ready_for_dev) feeds execution
// (ready_for_dev -> in_progress). blocked and in_review are both part of
// execution too (01-definicion-producto.md, section 8, groups "desarrollo,
// bloqueo, code review" as one phase), so blocked is reachable from either
// in_progress or in_review, and always returns to in_progress: whatever the
// blocker was, resuming means going back to work, not straight back into
// review. From in_review, work either returns to in_progress (changes
// requested) or leaves execution, either straight to completed or into the
// per-environment loop (deploying <-> testing, one round per environment,
// advancing forward on a pass and back to in_progress on a failure); the loop
// itself closes into completed. Both of those two completed edges carry a
// runtime condition — see CanTransition — beyond just being listed here.
// cancelled is reachable from every non-definitive state: abandoning a task
// is always legal until it is done.
//
// A bug is triaged into the same graph via triage -> ready_for_dev (queued
// like any other task) or triage -> in_progress (triaged straight into
// work); from there it follows the same rules as a task.
var taskTransitions = map[TaskState][]TaskState{
	TaskStateDraft:       {TaskStateReadyForDev, TaskStateCancelled},
	TaskStateReadyForDev: {TaskStateInProgress, TaskStateCancelled},
	TaskStateTriage:      {TaskStateReadyForDev, TaskStateInProgress, TaskStateCancelled},
	TaskStateInProgress:  {TaskStateBlocked, TaskStateInReview, TaskStateCancelled},
	TaskStateBlocked:     {TaskStateInProgress, TaskStateCancelled},
	TaskStateInReview:    {TaskStateInProgress, TaskStateBlocked, TaskStateDeploying, TaskStateCompleted, TaskStateCancelled},
	TaskStateDeploying:   {TaskStateTesting, TaskStateCancelled},
	TaskStateTesting:     {TaskStateDeploying, TaskStateInProgress, TaskStateCompleted, TaskStateCancelled},
	TaskStateCompleted:   {},
	TaskStateCancelled:   {},
}

// CanTransition reports whether moving t to "to" is legal. It takes the
// whole task, not just its type and state, because two of the graph's edges
// cannot be decided from those alone:
//
//   - in_review -> completed (closing without ever entering the loop) is
//     legal only when t has no declared ClosureEnvironment. A task that does
//     declare one has something to loop over, and 01-definicion-producto.md,
//     section 8 requires a human to confirm each environment in that loop —
//     skipping straight to completed would let that confirmation be skipped
//     too.
//   - testing -> completed is legal only when t.CurrentEnvironment equals
//     t.ClosureEnvironment: ClosureEnvironment names where the task counts
//     as closed, so completing off the back of a different environment's
//     test round would contradict what the field means.
//
// Both checks read fields the caller already carries in memory, so this
// stays pure. What it deliberately still cannot do: know the project's
// environments[] order. testing -> deploying (advancing to the next
// environment) is legal unconditionally, and walking the loop once per
// declared environment in order — stopping only once ClosureEnvironment is
// reached — is left to whichever caller does hold that list (the flow
// engine or the CLI, T-070/T-080).
//
// CanTransition itself only ever looks at one version of t. Deciding whether
// an update from one stored version to another is legal, when the two
// versions might disagree on ClosureEnvironment or CurrentEnvironment as
// well as on State, takes evaluating this same edge under both versions'
// environment fields — that composition lives in ValidateTaskUpdate, not
// here, because CanTransition has no second Task to compare against.
//
// triage is never a legal destination: it is a birth state only (see
// InitialState), and a task of type task is never legally in triage at all
// (task.schema.json: "Only bugs are born in triage"). An unrecognized
// TaskType is rejected outright rather than silently treated as a task.
func CanTransition(t Task, to TaskState) bool {
	if !t.Type.Valid() {
		return false
	}

	from := t.State
	if to == TaskStateTriage {
		return false
	}
	if from == TaskStateTriage && t.Type != TaskTypeBug {
		return false
	}

	edgeExists := false
	for _, candidate := range taskTransitions[from] {
		if candidate == to {
			edgeExists = true
			break
		}
	}
	if !edgeExists {
		return false
	}

	switch {
	case from == TaskStateInReview && to == TaskStateCompleted:
		return t.ClosureEnvironment == ""
	case from == TaskStateTesting && to == TaskStateCompleted:
		return t.ClosureEnvironment != "" && t.CurrentEnvironment == t.ClosureEnvironment
	default:
		return true
	}
}

// ValidateBlocked checks that a task claiming the blocked state actually
// names something it is waiting on. Enforcing this against a stored artifact
// is the schema's job (task.schema.json: "A blocked task names its
// blocker"); this gives the same invariant to a task built or updated purely
// in memory, before any serialization happens — the same defense-in-depth
// ValidateBirth gives to a bug's birth state.
//
// It is deliberately not folded into CanTransition: whether a task is
// internally consistent while blocked is a single-document question, not an
// edge of the state graph, and it has to hold not just at the moment of
// transitioning into blocked but for as long as the task stays there — a
// blocked task stripped of its only blocker while remaining blocked is just
// as illegal as never having named one. ApplyTransition and
// ValidateTaskUpdate both call it for exactly that reason.
func ValidateBlocked(t Task) error {
	if t.State == TaskStateBlocked && len(t.BlockedBy) == 0 {
		return errors.New("core: a blocked task must name at least one blocker")
	}
	return nil
}

// ApplyTransition moves a task/bug to a new state, appending a record of who
// did it, when and why to its history. It rejects a transition out of a
// definitive state, any transition the state graph does not allow, and
// landing in blocked without a blocker already named on the receiver, and
// returns a new Task rather than mutating the receiver.
func (t Task) ApplyTransition(to TaskState, by Actor, at time.Time, reason string) (Task, error) {
	if t.State.IsDefinitive() {
		return Task{}, fmt.Errorf("core: task is in definitive state %q, no transition is legal", t.State)
	}
	if !CanTransition(t, to) {
		return Task{}, fmt.Errorf("core: illegal transition from %q to %q for type %q", t.State, to, t.Type)
	}

	history := make([]StateTransition, len(t.StateHistory), len(t.StateHistory)+1)
	copy(history, t.StateHistory)
	history = append(history, StateTransition{
		From:   t.State,
		To:     to,
		By:     by,
		At:     at,
		Reason: reason,
	})

	next := t
	next.State = to
	next.StateHistory = history

	if err := ValidateBlocked(next); err != nil {
		return Task{}, err
	}

	return next, nil
}

// EnterEnvironment moves a task into deploying for a specific environment.
// This is the domain operation for changing CurrentEnvironment: entering
// deploying always means entering it for one concrete environment, so the
// environment is an argument of the operation instead of a field a caller
// assigns on its own afterward — the same discipline ApplyTransition and
// ValidateBlocked already give BlockedBy. Both the first round of the
// per-environment loop (from in_review) and every later round (from
// testing) go through this one method; ApplyTransition/CanTransition still
// decide whether entering deploying is legal from wherever t currently is.
func (t Task) EnterEnvironment(environment string, by Actor, at time.Time, reason string) (Task, error) {
	if environment == "" {
		return Task{}, errors.New("core: entering the per-environment loop requires naming an environment")
	}

	next, err := t.ApplyTransition(TaskStateDeploying, by, at, reason)
	if err != nil {
		return Task{}, err
	}

	next.CurrentEnvironment = environment
	return next, nil
}

// ValidateTaskUpdate checks whether replacing previous with next is a legal
// change to a task/bug. Once previous is in a definitive state, no field of
// it may differ in next: reaching completed or cancelled is a one-document
// rule the schema can express (state has no outgoing transition), but
// rejecting a later edit to the same document takes comparing two versions of
// it, which only code can do (schemas/README.md).
//
// Short of that, any state change between the two versions must itself be a
// legal edge of the same graph ApplyTransition enforces — this is the entry
// point store reaches for when validating an incoming write, not just the
// happy path of building one up through ApplyTransition calls, so it has to
// hold the same rule.
//
// That check alone is not enough for the two edges into completed, which
// CanTransition gates on ClosureEnvironment/CurrentEnvironment (see its
// comment): evaluating the edge against previous's environment fields only
// would let a single write add a fresh ClosureEnvironment in the same update
// that also completes the task — previous had none, so previous's fields say
// the direct close is legal, and next's declared environment along for the
// ride is never checked against anything. Evaluating against next's fields
// instead just moves the smuggling the other way: previous can declare a
// ClosureEnvironment, and the same write that completes the task erases it,
// so next's fields say the direct close is legal too. Neither version alone
// is trustworthy, because either one might be the side of the write that
// changed the very field the check depends on. The edge has to stay legal
// under both previous's environment fields (already covered by the
// CanTransition(previous, ...) call above) and under next's — computed by
// re-running the same check on previous's state with next's environment
// fields swapped in, since it is still the previous->next edge being judged,
// just against the other version's data.
//
// Finally, next must itself be internally consistent about being blocked,
// whether or not this update is what moved it there.
func ValidateTaskUpdate(previous, next Task) error {
	if previous.State.IsDefinitive() {
		if !reflect.DeepEqual(previous, next) {
			return fmt.Errorf("core: task is in definitive state %q, no further change is legal", previous.State)
		}
		return nil
	}

	if previous.State != next.State {
		if !CanTransition(previous, next.State) {
			return fmt.Errorf("core: illegal transition from %q to %q for type %q", previous.State, next.State, previous.Type)
		}

		withNextEnvironment := previous
		withNextEnvironment.ClosureEnvironment = next.ClosureEnvironment
		withNextEnvironment.CurrentEnvironment = next.CurrentEnvironment
		if !CanTransition(withNextEnvironment, next.State) {
			return fmt.Errorf("core: illegal transition from %q to %q for type %q", previous.State, next.State, previous.Type)
		}
	}

	return ValidateBlocked(next)
}
