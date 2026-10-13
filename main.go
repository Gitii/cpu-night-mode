package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "/etc/cpu-night-mode.conf", "configuration file")
	sysfs := flag.String("sysfs", "/sys/devices/system/cpu/cpufreq", "CPU frequency policy directory")
	stateDir := flag.String("state-dir", "/run/cpu-night-mode", "recovery state directory")
	flag.Parse()
	if flag.NArg() != 1 {
		return fmt.Errorf("usage: cpu-night-mode [flags] daemon|status|check-config|restore|version")
	}
	command := flag.Arg(0)
	if command == "version" {
		fmt.Println(version)
		return nil
	}
	if command != "daemon" && command != "status" && command != "check-config" && command != "restore" {
		return fmt.Errorf("unknown command %q", command)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	c := controller{Sysfs: *sysfs, StateDir: *stateDir, BootID: strings.TrimSpace(string(boot))}
	if command == "restore" {
		lock, err := c.lock()
		if err != nil {
			return err
		}
		defer lock.Close()
		return c.restore()
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if command == "check-config" {
		fmt.Printf("valid: %s–%s %s, maximum %d MHz\n", timeString(cfg.Start), timeString(cfg.End), cfg.Timezone, cfg.MaxMHz)
		return nil
	}
	if command == "status" {
		return status(c, cfg)
	}
	lock, err := c.lock()
	if err != nil {
		return err
	}
	defer lock.Close()
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(signals)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	last := ""
	reconcile := func() error {
		active := cfg.active(time.Now())
		mode := "day"
		if active {
			mode = fmt.Sprintf("night (maximum %d MHz)", cfg.MaxMHz)
			if err := c.apply(cfg.MaxMHz); err != nil {
				return err
			}
		} else if err := c.restore(); err != nil {
			return err
		}
		if mode != last {
			log.Printf("%s: %s–%s %s", mode, timeString(cfg.Start), timeString(cfg.End), cfg.Timezone)
			last = mode
		}
		return nil
	}
	if err := reconcile(); err != nil {
		return err
	}
	for {
		select {
		case sig := <-signals:
			if sig != syscall.SIGHUP {
				return c.restore()
			}
			next, err := loadConfig(*configPath)
			if err != nil {
				log.Printf("reload rejected; retaining configuration: %v", err)
				continue
			}
			cfg = next
			log.Print("configuration reloaded")
		case <-ticker.C:
		}
		if err := reconcile(); err != nil {
			return err
		}
	}
}

func status(c controller, cfg config) error {
	now := time.Now().In(cfg.Location)
	fmt.Printf("Time: %s\nSchedule: %s–%s %s\nConfigured ceiling: %d MHz\nInside schedule: %t\n", now.Format(time.RFC3339), timeString(cfg.Start), timeString(cfg.End), cfg.Timezone, cfg.MaxMHz, cfg.active(now))
	s, err := c.loadState()
	if err != nil {
		return err
	}
	fmt.Printf("Recovery state present: %t\n", s != nil)
	policies, err := c.policies()
	if err != nil {
		return err
	}
	for _, p := range policies {
		max, err := readNumber(c.attr(p, "scaling_max_freq"))
		if err != nil {
			return err
		}
		fmt.Printf("%s: maximum %d MHz", p, max/1000)
		if s != nil {
			if original, ok := s.Maxima[p]; ok {
				fmt.Printf(" (saved %d MHz)", original/1000)
			}
		}
		fmt.Println()
	}
	return nil
}
