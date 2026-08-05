// Package citest keeps the CI pipeline and the Makefile from drifting apart,
// as an executable test rather than as a comment asking future readers to
// remember.
//
// It holds no production code: the rules live entirely in pipeline_test.go.
package citest
