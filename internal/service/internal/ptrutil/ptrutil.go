// Package ptrutil holds tiny generic pointer helpers shared across the service
// layer, replacing the per-package iptr/fptr/intPtr/floatPtr one-liners.
package ptrutil

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }
