package core

// ActorKind distinguishes a human from an agent as the origin of a change.
type ActorKind string

const (
	// ActorKindHuman identifies a person, attributed by the project's git identity.
	ActorKindHuman ActorKind = "human"
	// ActorKindAgent identifies an agent, attributed by the display name the
	// project declared for it.
	ActorKindAgent ActorKind = "agent"
)

// Actor is who caused a change: a human or an agent. It is passed in by the
// caller wherever a domain rule needs to know who acted, since core performs
// no I/O and cannot look up an identity on its own.
type Actor struct {
	Kind ActorKind
	Name string
}
