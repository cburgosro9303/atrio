// Package web will embed the compiled React SPA of the local portal into the
// single Atrio binary, and serve it as a static filesystem.
//
// The embed directive is intentionally absent until the frontend build exists:
// //go:embed fails at compile time when its target directory is missing, and
// the build output directory is git-ignored, so an empty placeholder could not
// be committed to satisfy it either. The directive lands together with the
// React + TypeScript + Vite toolchain.
package web
