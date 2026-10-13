package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T) controller {
	t.Helper()
	root := t.TempDir()
	c := controller{Sysfs: filepath.Join(root, "sysfs"), StateDir: filepath.Join(root, "state"), BootID: "test-boot"}
	if err := os.MkdirAll(c.StateDir, 0755); err != nil {
		t.Fatal(err)
	}
	addPolicy(t, c, "policy0", "800000", "3700000")
	addPolicy(t, c, "policy1", "800000", "1400000")
	return c
}

func addPolicy(t *testing.T, c controller, p, minimum, maximum string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(c.Sysfs, p), 0755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"scaling_min_freq": minimum, "scaling_max_freq": maximum} {
		if err := os.WriteFile(c.attr(p, name), []byte(value+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func assertMax(t *testing.T, c controller, p string, want int64) {
	t.Helper()
	got, err := readNumber(c.attr(p, "scaling_max_freq"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s maximum = %d, want %d", p, got, want)
	}
}

func TestRestartReloadHotplugAndRestore(t *testing.T) {
	c := fixture(t)
	if err := c.apply(1600); err != nil {
		t.Fatal(err)
	}
	assertMax(t, c, "policy0", 1600000)
	assertMax(t, c, "policy1", 1400000) // Never raise an existing lower limit.
	// A fresh controller must recover the original baseline, not snapshot the cap.
	c = controller{Sysfs: c.Sysfs, StateDir: c.StateDir, BootID: c.BootID}
	if err := c.apply(1200); err != nil {
		t.Fatal(err)
	}
	if err := c.apply(2000); err != nil {
		t.Fatal(err)
	}
	assertMax(t, c, "policy0", 2000000)
	assertMax(t, c, "policy1", 1400000)
	addPolicy(t, c, "policy2", "800000", "3500000")
	if err := c.apply(1600); err != nil {
		t.Fatal(err)
	}
	assertMax(t, c, "policy2", 1600000)
	if err := c.restore(); err != nil {
		t.Fatal(err)
	}
	assertMax(t, c, "policy0", 3700000)
	assertMax(t, c, "policy1", 1400000)
	assertMax(t, c, "policy2", 3500000)
	if _, err := os.Stat(c.statePath()); !os.IsNotExist(err) {
		t.Fatalf("state remains: %v", err)
	}
	if err := c.restore(); err != nil {
		t.Fatal(err)
	}
}

func TestValidationBeforeAnyWrite(t *testing.T) {
	c := fixture(t)
	addPolicy(t, c, "policy2", "2000000", "3700000")
	if err := c.apply(1600); err == nil {
		t.Fatal("accepted cap below minimum")
	}
	assertMax(t, c, "policy0", 3700000)
	if _, err := os.Stat(c.statePath()); !os.IsNotExist(err) {
		t.Fatalf("saved invalid transaction: %v", err)
	}
}

func TestPartialWriteRollbackAndFailedRestore(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failures require an unprivileged test process")
	}
	c := fixture(t)
	// The last policy fails after the first policy has been capped.
	path := c.attr("policy1", "scaling_max_freq")
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatal(err)
	}
	if err := c.apply(1200); err == nil {
		t.Fatal("expected write failure")
	}
	assertMax(t, c, "policy0", 3700000)
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.apply(1200); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatal(err)
	}
	if err := c.restore(); err == nil {
		t.Fatal("expected restore failure")
	}
	if _, err := os.Stat(c.statePath()); err != nil {
		t.Fatalf("lost recovery state: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := c.restore(); err != nil {
		t.Fatal(err)
	}
	assertMax(t, c, "policy1", 1400000)
}

func TestRejectUntrustedRecoveryState(t *testing.T) {
	c := fixture(t)
	for _, text := range []string{
		`{"version":1,"boot_id":"other-boot","maxima_khz":{"policy0":3700000}}`,
		`{"version":1,"boot_id":"test-boot","maxima_khz":{"../outside":3700000}}`,
		`{"version":1,"boot_id":"test-boot","maxima_khz":{"policy0":0}}`,
		`{"version":2,"boot_id":"test-boot","maxima_khz":{"policy0":3700000}}`,
		`{"version":1,"boot_id":"test-boot","maxima_khz":{}}`,
		`not json`,
	} {
		if err := os.WriteFile(c.statePath(), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
		if err := c.restore(); err == nil {
			t.Errorf("accepted state %s", text)
		}
		assertMax(t, c, "policy0", 3700000)
	}
}

func TestSingleWriterLock(t *testing.T) {
	c := fixture(t)
	first, err := c.lock()
	if err != nil {
		t.Fatal(err)
	}
	if second, err := c.lock(); err == nil {
		second.Close()
		t.Fatal("second writer acquired lock")
	}
	first.Close()
	third, err := c.lock()
	if err != nil {
		t.Fatal(err)
	}
	third.Close()
}

func TestWaitForQueuedKernelUpdate(t *testing.T) {
	c := fixture(t)
	path := c.attr("policy0", "scaling_max_freq")
	done := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		// Rename avoids torn reads in the fake filesystem's delayed update.
		tmp := path + ".next"
		if err := os.WriteFile(tmp, []byte("1600000\n"), 0644); err != nil {
			done <- err
			return
		}
		done <- os.Rename(tmp, path)
	}()
	if err := waitMax("policy0", path, 1600000, false); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRestoreWritesEvenIfVisibleValueMatches(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failures require an unprivileged test process")
	}
	c := fixture(t)
	s := &snapshot{Version: 1, BootID: c.BootID, Maxima: map[string]int64{"policy0": 3700000}}
	if err := c.saveState(s); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(c.attr("policy0", "scaling_max_freq"), 0444); err != nil {
		t.Fatal(err)
	}
	// A matching readback cannot rule out a queued cap. Restore must still write.
	if err := c.restore(); err == nil {
		t.Fatal("restore skipped the write")
	}
	if _, err := os.Stat(c.statePath()); err != nil {
		t.Fatal("lost recovery state")
	}
}
