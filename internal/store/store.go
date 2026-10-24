// Package store keeps quota readings in one JSON Lines file per UTC day.
package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Reading is one quota window of one credential at one moment, in the shape
// of the panel's history snapshots.
type Reading struct {
	// TS is when the quota was read, in Unix milliseconds.
	TS int64 `json:"ts"`
	// Credential is the CPA credential file name.
	Credential string `json:"credential"`
	AuthIndex  string `json:"auth_index,omitempty"`
	Provider   string `json:"provider"`
	// Window is the panel's window ID, such as "five-hour" or "seven-day".
	Window string `json:"window"`
	// Remaining is the percent left, 0 to 100.
	Remaining   float64  `json:"remaining"`
	ResetAtMs   *int64   `json:"reset_at_ms"`
	PeriodHours *float64 `json:"period_hours"`
}

const dayLayout = "2006-01-02"

// Store reads and writes the daily files under Dir.
type Store struct {
	Dir string
	mu  sync.Mutex
}

// Append writes readings to the files of their days.
func (s *Store) Append(readings []Reading) error {
	if len(readings) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	byDay := map[string][]Reading{}
	for _, reading := range readings {
		day := time.UnixMilli(reading.TS).UTC().Format(dayLayout)
		byDay[day] = append(byDay[day], reading)
	}
	for day, items := range byDay {
		if err := s.appendFile(filepath.Join(s.Dir, day+".jsonl"), items); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) appendFile(path string, readings []Reading) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	encoder := json.NewEncoder(writer)
	for _, reading := range readings {
		if err := encoder.Encode(reading); err != nil {
			file.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Since returns the readings taken at or after sinceMs, oldest first.
func (s *Store) Since(sinceMs int64) ([]Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	firstDay := time.UnixMilli(sinceMs).UTC().Format(dayLayout)
	days, err := s.days()
	if err != nil {
		return nil, err
	}
	out := []Reading{}
	for _, day := range days {
		if day < firstDay {
			continue
		}
		readings, err := readFile(filepath.Join(s.Dir, day+".jsonl"))
		if err != nil {
			return nil, err
		}
		for _, reading := range readings {
			if reading.TS >= sinceMs {
				out = append(out, reading)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out, nil
}

// Prune deletes the files of days older than retention.
func (s *Store) Prune(now time.Time, retention time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldest := now.Add(-retention).UTC().Format(dayLayout)
	days, err := s.days()
	if err != nil {
		return err
	}
	for _, day := range days {
		if day < oldest {
			if err := os.Remove(filepath.Join(s.Dir, day+".jsonl")); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

// days lists the stored days, oldest first.
func (s *Store) days() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var days []string
	for _, entry := range entries {
		day, ok := strings.CutSuffix(entry.Name(), ".jsonl")
		if _, err := time.Parse(dayLayout, day); ok && err == nil {
			days = append(days, day)
		}
	}
	sort.Strings(days)
	return days, nil
}

func readFile(path string) ([]Reading, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var readings []Reading
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var reading Reading
		// A line cut short by a crash is skipped, not fatal.
		if json.Unmarshal(scanner.Bytes(), &reading) == nil {
			readings = append(readings, reading)
		}
	}
	return readings, scanner.Err()
}
