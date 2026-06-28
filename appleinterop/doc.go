// Package appleinterop holds the macOS-only interoperability guard test that
// links against Apple's system libcompression and verifies byte-exact two-way
// interop with the parent lzfse package. The actual test is in a file tagged
// `darwin && cgo`, so on every other platform this package is empty (this file
// keeps it buildable). It is a separate package because cgo is not permitted in
// the parent package's own _test.go files while that package builds with
// CGO_ENABLED=0.
package appleinterop
