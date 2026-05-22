package app_test

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/app"
)

func TestAcquireSingleInstanceLock_Contention(t *testing.T) {
	dir := t.TempDir()

	release1, acquired1, err := app.AcquireSingleInstanceLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if !acquired1 {
		t.Fatalf("first acquire should succeed on a fresh dir")
	}
	defer release1()

	// A second acquire on the same fd-backed lock within the SAME process
	// still observes the held flock and must report not-acquired.
	release2, acquired2, err := app.AcquireSingleInstanceLock(dir)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if acquired2 {
		if release2 != nil {
			release2()
		}
		t.Fatalf("second acquire should fail while the first holds the lock")
	}

	release1()
	release3, acquired3, err := app.AcquireSingleInstanceLock(dir)
	if err != nil {
		t.Fatalf("third acquire: %v", err)
	}
	if !acquired3 {
		t.Fatalf("acquire should succeed after the first is released")
	}
	release3()
}
