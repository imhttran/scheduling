package main

import (
	"testing"
	"time"
)

func TestWorkerPolicyFor(t *testing.T) {
	cases := []struct {
		name     string
		wt       string
		hb       int
		regular  float64
		overtime bool
		cap      float64
	}{
		{"student", "student", 0, 20, false, 20},
		{"fulltime", "fulltime", 0, 40, true, 60},
		{"hourly default", "hourly", 0, 40, true, 60},
		{"hourly lowered", "hourly", 32, 32, true, 60},
		{"hourly ignores too-high limit", "hourly", 45, 40, true, 60},
		{"unknown falls back to student", "manager", 0, 20, false, 20},
	}
	for _, c := range cases {
		p := workerPolicyFor(c.wt, c.hb)
		if p.regular != c.regular || p.overtime != c.overtime || p.cap != c.cap {
			t.Errorf("workerPolicyFor(%s, %d) = regular %v overtime %v cap %v; want regular %v overtime %v cap %v",
				c.name, c.hb, p.regular, p.overtime, p.cap, c.regular, c.overtime, c.cap)
		}
	}
}

func TestWeekMondayOf(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"2026-08-31", "2026-08-31"}, // Monday
		{"2026-08-30", "2026-08-24"}, // Sunday -> previous Monday
		{"2026-09-06", "2026-08-31"}, // Sunday of a Mon-Sun week
		{"2026-09-07", "2026-09-07"}, // next Monday
	}
	layout := "2006-01-02"
	for _, c := range cases {
		in, _ := time.Parse(layout, c.in)
		if got := weekMondayOf(in).Format(layout); got != c.want {
			t.Errorf("weekMondayOf(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestWorkerTypeLabel(t *testing.T) {
	cases := map[string]string{
		"student":  "Student",
		"fulltime": "Full-time staff",
		"hourly":   "Hourly staff",
		"bogus":    "Student",
	}
	for in, want := range cases {
		if got := workerTypeLabel(in); got != want {
			t.Errorf("workerTypeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
