package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type config struct {
	Start, End int
	Timezone   string
	Location   *time.Location
	MaxMHz     int64
}

func clock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != 5 {
		return 0, fmt.Errorf("invalid time %q: expected HH:MM", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

func loadConfig(path string) (config, error) {
	c := config{Start: 19 * 60, End: 9 * 60, Timezone: "Europe/Berlin", MaxMHz: 1600}
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	section := ""
	seen := map[string]bool{}
	for line := 1; scanner.Scan(); line++ {
		s := strings.TrimSpace(scanner.Text())
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, ";") {
			continue
		}
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			section = s[1 : len(s)-1]
			if section != "schedule" && section != "cpu" {
				return c, fmt.Errorf("line %d: unknown section %q", line, section)
			}
			continue
		}
		key, value, ok := strings.Cut(s, "=")
		if !ok {
			return c, fmt.Errorf("line %d: expected key = value", line)
		}
		key, value = section+"."+strings.TrimSpace(key), strings.TrimSpace(value)
		if seen[key] {
			return c, fmt.Errorf("line %d: duplicate key %s", line, key)
		}
		seen[key] = true
		switch key {
		case "schedule.start":
			c.Start, err = clock(value)
		case "schedule.end":
			c.End, err = clock(value)
		case "schedule.timezone":
			c.Timezone = value
		case "cpu.max_frequency_mhz":
			c.MaxMHz, err = strconv.ParseInt(value, 10, 64)
		default:
			return c, fmt.Errorf("line %d: unknown key %s", line, key)
		}
		if err != nil {
			return c, fmt.Errorf("line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return c, err
	}
	if c.Start == c.End {
		return c, fmt.Errorf("start and end must differ")
	}
	if c.MaxMHz <= 0 || c.MaxMHz > 100000 {
		return c, fmt.Errorf("max_frequency_mhz must be between 1 and 100000")
	}
	if c.Timezone == "" || c.Timezone == "Local" {
		return c, fmt.Errorf("timezone must be a named IANA timezone")
	}
	c.Location, err = time.LoadLocation(c.Timezone)
	if err != nil {
		return c, fmt.Errorf("timezone: %w", err)
	}
	return c, nil
}

func (c config) active(now time.Time) bool {
	t := now.In(c.Location)
	m := t.Hour()*60 + t.Minute()
	if c.Start < c.End {
		return m >= c.Start && m < c.End
	}
	return m >= c.Start || m < c.End
}

func timeString(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }
