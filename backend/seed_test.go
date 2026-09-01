package main

import "testing"

func TestPlanAssignmentsTarget(t *testing.T) {
	// 10 shifts of 8h = 80h total; 80% target = 64h = 8 shifts. The worker can
	// take 5 shifts/week across 2 weeks (cap 40h), so it can reach the target.
	var shifts []assignShift
	for i := 0; i < 10; i++ {
		shifts = append(shifts, assignShift{id: i, job: 1, week: i / 5, hours: 8})
	}
	workers := []assignWorker{{id: 100, jobs: map[int]bool{1: true}, cap: 40}}
	plan := planAssignments(shifts, workers, 0.8)
	if len(plan) != 8 {
		t.Fatalf("plan assigned %d shifts, want 8", len(plan))
	}
}

func TestPlanAssignmentsRespectsCap(t *testing.T) {
	shifts := []assignShift{{id: 1, job: 1, week: 0, hours: 10}}
	workers := []assignWorker{{id: 100, jobs: map[int]bool{1: true}, cap: 8}}
	plan := planAssignments(shifts, workers, 0.8)
	if len(plan) != 0 {
		t.Fatalf("plan assigned a shift exceeding the worker cap, got %d", len(plan))
	}
}

func TestPlanAssignmentsRespectsJob(t *testing.T) {
	shifts := []assignShift{{id: 1, job: 2, week: 0, hours: 8}}
	workers := []assignWorker{{id: 100, jobs: map[int]bool{1: true}, cap: 40}}
	plan := planAssignments(shifts, workers, 0.8)
	if len(plan) != 0 {
		t.Fatalf("plan assigned a shift for a job the worker isn't qualified for, got %d", len(plan))
	}
}

func TestPlanAssignmentsWeeklyCap(t *testing.T) {
	// Two 8h shifts in the same week, worker cap 8h -> only one assigned.
	shifts := []assignShift{
		{id: 1, job: 1, week: 0, hours: 8},
		{id: 2, job: 1, week: 0, hours: 8},
	}
	workers := []assignWorker{{id: 100, jobs: map[int]bool{1: true}, cap: 8}}
	plan := planAssignments(shifts, workers, 0.8)
	if len(plan) != 1 {
		t.Fatalf("plan assigned %d shifts, want 1 (weekly cap)", len(plan))
	}
}
