package state

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireLeaseExclusive(t *testing.T) {
	s := newTestStore(t)
	l := &Lease{Owner: "a", PID: os.Getpid(), Host: "h", Expires: time.Now().Add(time.Hour)}

	if err := s.AcquireLease("x", l); err != nil {
		t.Fatal(err)
	}
	if err := s.AcquireLease("x", l); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("second acquire: got %v, want ErrLeaseHeld", err)
	}
	got, err := s.ReadLease("x")
	if err != nil || got == nil || got.Owner != "a" {
		t.Fatalf("read: %+v %v", got, err)
	}
	if err := s.ReleaseLease("x"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseLease("x"); err != nil {
		t.Fatalf("release is not idempotent: %v", err)
	}
	if err := s.AcquireLease("x", l); err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
}

func TestWriteLeaseOverwritesStaleFile(t *testing.T) {
	s := newTestStore(t)
	// AcquireLease leaves a file behind (Reconcile only clears the manifest
	// mirror), so claim/renew must be able to overwrite it.
	if err := s.AcquireLease("x", &Lease{Owner: "old", PID: 1, Host: "h", Expires: time.Now()}); err != nil {
		t.Fatal(err)
	}
	durable := &Lease{Owner: "new", PID: 0, Host: "h", Expires: time.Now().Add(time.Hour)}
	if err := s.WriteLease("x", durable); err != nil {
		t.Fatalf("WriteLease over a stale file: %v", err)
	}
	got, err := s.ReadLease("x")
	if err != nil || got == nil {
		t.Fatalf("read: %+v %v", got, err)
	}
	if got.Owner != "new" || got.PID != 0 {
		t.Errorf("lease not overwritten: %+v", got)
	}
}

func TestAcquireLeaseConcurrent(t *testing.T) {
	s := newTestStore(t)
	const n = 24
	var wins int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.AcquireLease("race", &Lease{Owner: "o"}); err == nil {
				atomic.AddInt64(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("exactly one concurrent acquire should win, got %d", wins)
	}
}
