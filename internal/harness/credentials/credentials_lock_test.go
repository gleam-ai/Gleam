package credentials

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"gleam/internal/filelock"
)

// setKey retries on ErrLocked. The advisory lock is unfair (Windows LockFileEx
// has no wait queue), so under full-suite parallel load a writer can be briefly
// starved; that is transient contention, not a lost update. Retrying isolates
// the property under test (completed writes never clobber each other) from
// load-induced starvation. Without the cross-process lock SetLLMAPIKey never
// returns ErrLocked, so a real lost update still surfaces as a missing key.
// The retry bound is a time budget, not an attempt count: how many attempts
// starvation lasts depends on machine load, while a few seconds is far longer
// than any honest writer needs and still ends the test on a real deadlock.
func setKey(s *Store, key, host string) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := s.SetLLMAPIKey(key, host)
		if !errors.Is(err, filelock.ErrLocked) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestStore_CrossInstanceNoLostUpdate simulates two sidecars sharing a data dir:
// independent Store instances (separate in-process mutexes) writing distinct
// hosts concurrently must not clobber each other. Without the cross-process
// file lock the read-modify-write cycles interleave and drop keys.
func TestStore_CrossInstanceNoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	const hosts = 4
	const rounds = 12
	var wg sync.WaitGroup
	for i := 0; i < hosts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Open(dir)
			if err != nil {
				t.Error(err)
				return
			}
			host := fmt.Sprintf("h%d.example.test", i)
			for j := 0; j < rounds; j++ {
				if err := setKey(s, fmt.Sprintf("k%d-%d", i, j), host); err != nil {
					t.Error(err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < hosts; i++ {
		host := fmt.Sprintf("h%d.example.test", i)
		if k, _ := s.ResolveLLMAPIKey(host); k == "" {
			t.Errorf("lost key for %s: concurrent writers clobbered it", host)
		}
	}
}

// TestStore_SessionSurvivesConcurrentKeyWrites proves the session and the key
// map share one lock: a key writer must not roll back a session another instance
// just stored.
func TestStore_SessionSurvivesConcurrentKeyWrites(t *testing.T) {
	dir := t.TempDir()
	owner, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.SetSession(&CloudSession{Email: "user@example.test", AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := Open(dir)
			if err != nil {
				t.Error(err)
				return
			}
			for j := 0; j < 12; j++ {
				if err := setKey(s, fmt.Sprintf("k%d", j), fmt.Sprintf("h%d.example.test", i)); err != nil {
					t.Error(err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	check, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess := check.GetSession()
	if sess == nil || sess.AccessToken != "tok" {
		t.Errorf("session was clobbered by concurrent key writes: %+v", sess)
	}
}
