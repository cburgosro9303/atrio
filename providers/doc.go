// Package providers defines the provider-agnostic adapter contract for the AI
// agent CLIs installed on the machine, plus the registry that makes them
// discoverable.
//
// An adapter carries five responsibilities: identity and discovery, capability
// reporting against a closed catalog governed by the platform, materialization
// of canonical agent definitions into the provider's native dialect (with drift
// reporting), execution in headless and interactive modes emitting a normalized
// event stream, and token metrics when the provider exposes them.
//
// Two rules stay outside the adapters: permission arbitration and capability
// degradation countermeasures both live in core, so behavior is identical
// across providers. Adding a provider means implementing the interface and
// registering it — zero changes to core, flows or commands.
//
// Provider implementations live in subpackages; the first one, claudecode, is
// introduced by task T-051.
package providers
