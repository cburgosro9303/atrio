// Package gitops wraps the system git binary, which is the backbone of
// isolation, traceability and team synchronization. Embedded git libraries are
// deliberately not used.
//
// Security rule: git is always invoked with an argument array, never through an
// interpolated shell string. Machine-readable output is obtained with
// --porcelain rather than parsed from human-facing text.
//
// Responsibilities: binary detection and minimum-version check, git identity
// validation, branch and worktree management for the one-task-one-branch model,
// and governed commit and push operations.
package gitops
