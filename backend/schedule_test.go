package main

import (
	"testing"
	"time"
)

func TestWeekDate(t *testing.T) {
	// Monday 2026-08-31.
	monday := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		dow  int
		want string
	}{
		{1, "2026-08-31"}, // Monday
		{0, "2026-09-06"}, // Sunday
		{6, "2026-09-05"}, // Saturday
	}
	for _, c := range cases {
		if got := weekDate(monday, c.dow).Format("2006-01-02"); got != c.want {
			t.Errorf("weekDate(monday, %d) = %s, want %s", c.dow, got, c.want)
		}
	}
}

func TestHoursBetween(t *testing.T) {
	if got := hoursBetween("09:00", "17:00"); got != 8 {
		t.Errorf("hoursBetween(09:00,17:00) = %v, want 8", got)
	}
	if got := hoursBetween("09:00", "09:00"); got != 0 {
		t.Errorf("hoursBetween(09:00,09:00) = %v, want 0", got)
	}
	// DB TIME columns scan with seconds/microseconds — must still parse.
	if got := hoursBetween("08:00:00.000000", "14:00:00.000000"); got != 6 {
		t.Errorf("hoursBetween(08:00:00.000000,14:00:00.000000) = %v, want 6", got)
	}
}

func TestParseTime(t *testing.T) {
	if got := parseTime("9:00"); got != "09:00" {
		t.Errorf("parseTime(9:00) = %q, want 09:00", got)
	}
	if got := parseTime("bogus"); got != "" {
		t.Errorf("parseTime(bogus) = %q, want empty", got)
	}
}

func TestValidSchedules(t *testing.T) {
	good := []jobScheduleInput{
		{DayOfWeek: 1, StartTime: "09:00", EndTime: "17:00"},
		{DayOfWeek: 5, StartTime: "10:00", EndTime: "14:00"},
	}
	if !validSchedules(good) {
		t.Error("validSchedules(valid) = false, want true")
	}
	cases := []struct {
		name string
		in   []jobScheduleInput
	}{
		{"duplicate day", []jobScheduleInput{{DayOfWeek: 1, StartTime: "09:00", EndTime: "17:00"}, {DayOfWeek: 1, StartTime: "10:00", EndTime: "14:00"}}},
		{"out of range day", []jobScheduleInput{{DayOfWeek: 7, StartTime: "09:00", EndTime: "17:00"}}},
		{"end before start", []jobScheduleInput{{DayOfWeek: 1, StartTime: "17:00", EndTime: "09:00"}}},
		{"bad time", []jobScheduleInput{{DayOfWeek: 1, StartTime: "bogus", EndTime: "17:00"}}},
	}
	for _, c := range cases {
		if validSchedules(c.in) {
			t.Errorf("validSchedules(%s) = true, want false", c.name)
		}
	}
}

func TestValidHolidays(t *testing.T) {
	if !validHolidays([]jobHolidayInput{{Date: "2026-12-25", Reason: "Christmas"}}) {
		t.Error("validHolidays(valid) = false, want true")
	}
	cases := []struct {
		name string
		in   []jobHolidayInput
	}{
		{"bad date", []jobHolidayInput{{Date: "12/25/2026"}}},
		{"duplicate date", []jobHolidayInput{{Date: "2026-12-25"}, {Date: "2026-12-25"}}},
	}
	for _, c := range cases {
		if validHolidays(c.in) {
			t.Errorf("validHolidays(%s) = true, want false", c.name)
		}
	}
}
