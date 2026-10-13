package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type snapshot struct {
	Version int              `json:"version"`
	BootID  string           `json:"boot_id"`
	Maxima  map[string]int64 `json:"maxima_khz"`
}

type controller struct {
	Sysfs, StateDir, BootID string
}

var policyName = regexp.MustCompile(`^policy[0-9]+$`)

func readNumber(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid frequency in %s", path)
	}
	return n, nil
}

func (c controller) attr(policy, name string) string { return filepath.Join(c.Sysfs, policy, name) }
func (c controller) statePath() string               { return filepath.Join(c.StateDir, "originals.json") }

func (c controller) lock() (*os.File, error) {
	if err := os.MkdirAll(c.StateDir, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(c.StateDir, "controller.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another controller is running: %w", err)
	}
	return f, nil
}

func (c controller) policies() ([]string, error) {
	entries, err := os.ReadDir(c.Sysfs)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if policyName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no CPU frequency policies in %s", c.Sysfs)
	}
	return names, nil
}

func (c controller) loadState() (*snapshot, error) {
	f, err := os.Open(c.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var s snapshot
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return nil, fmt.Errorf("read recovery state: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing recovery state data")
	}
	if s.Version != 1 || s.BootID != c.BootID || len(s.Maxima) == 0 {
		return nil, fmt.Errorf("invalid recovery state or boot ID")
	}
	for p, max := range s.Maxima {
		if !policyName.MatchString(p) || max <= 0 {
			return nil, fmt.Errorf("invalid recovery policy %q", p)
		}
	}
	return &s, nil
}

func (c controller) saveState(s *snapshot) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(c.StateDir, ".originals-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0644); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), c.statePath())
}

func (c controller) setMax(policy string, target int64, exact bool) error {
	path := c.attr(policy, "scaling_max_freq")
	current, err := readNumber(path)
	if err != nil {
		return err
	}
	if current == target && !exact {
		return nil
	}
	if err := os.WriteFile(path, []byte(strconv.FormatInt(target, 10)+"\n"), 0644); err != nil {
		return err
	}
	return waitMax(policy, path, target, exact)
}

func waitMax(policy, path string, target int64, exact bool) error {
	// CPUFreq QoS changes can be queued asynchronously. Restoration must write
	// even when the visible value already matches, to supersede a pending cap.
	var actual int64
	for attempt := 0; attempt < 41; attempt++ {
		var err error
		actual, err = readNumber(path)
		if err != nil {
			return err
		}
		if actual == target || (!exact && actual < target) {
			return nil
		}
		if attempt < 40 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return fmt.Errorf("%s: requested %d kHz, read back %d after waiting for kernel update", policy, target, actual)
}

func (c controller) restore() error {
	s, err := c.loadState()
	if err != nil || s == nil {
		return err
	}
	var failures []error
	for p, max := range s.Maxima {
		if err := c.setMax(p, max, true); err != nil {
			failures = append(failures, fmt.Errorf("restore %s: %w", p, err))
		}
	}
	if err := errors.Join(failures...); err != nil {
		return err
	}
	return os.Remove(c.statePath())
}

func (c controller) apply(maxMHz int64) error {
	policies, err := c.policies()
	if err != nil {
		return err
	}
	s, err := c.loadState()
	if err != nil {
		return err
	}
	if s == nil {
		s = &snapshot{Version: 1, BootID: c.BootID, Maxima: map[string]int64{}}
	}
	targets := map[string]int64{}
	changedState := false
	for _, p := range policies {
		current, err := readNumber(c.attr(p, "scaling_max_freq"))
		if err != nil {
			return err
		}
		original, ok := s.Maxima[p]
		if !ok {
			original = current
			s.Maxima[p] = original
			changedState = true
		}
		target := min(original, maxMHz*1000)
		minimum, err := readNumber(c.attr(p, "scaling_min_freq"))
		if err != nil {
			return err
		}
		if target < minimum {
			return fmt.Errorf("%s: cap %d kHz below current minimum %d", p, target, minimum)
		}
		targets[p] = target
	}
	// Persist the complete baseline before the first hardware write.
	if changedState {
		if err := c.saveState(s); err != nil {
			return err
		}
	}
	for _, p := range policies {
		if err := c.setMax(p, targets[p], false); err != nil {
			return errors.Join(fmt.Errorf("apply %s: %w", p, err), c.restore())
		}
	}
	return nil
}
