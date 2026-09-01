package main

import (
	"context"
	"testing"
	"time"
)

func TestDefaultConfigMatchesWeeklyCapacity(t *testing.T) {
	config := defaultConfig()
	if err := validateForScheduling(config, schedulableSlots(config)); err != nil {
		t.Fatalf("default config should be schedulable: %v", err)
	}
	if got, want := len(config.Classes), 21; got != want {
		t.Fatalf("class count = %d, want %d", got, want)
	}
	for _, class := range config.Classes {
		total := 0
		for _, assignment := range class.Assignments {
			total += assignment.WeeklyLessons()
		}
		if total != 45 {
			t.Fatalf("%s weekly lessons = %d, want 45", class.Name, total)
		}
	}
}

func TestGenerateDefaultSchedule(t *testing.T) {
	config := defaultConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	schedules, report, err := GenerateSchedule(ctx, config, nil)
	if err != nil {
		t.Fatalf("generate schedule: %v", err)
	}
	if !report.Passed {
		t.Fatalf("validation failed: %+v", report)
	}
	if got, want := len(schedules), len(config.Classes); got != want {
		t.Fatalf("schedule count = %d, want %d", got, want)
	}
}

func TestValidationDetectsTeacherConflict(t *testing.T) {
	config := defaultConfig()
	schedules, _, err := GenerateSchedule(context.Background(), config, nil)
	if err != nil {
		t.Fatalf("generate schedule: %v", err)
	}
	var first *ScheduleCell
	for index := range schedules[0].Cells {
		if schedules[0].Cells[index].Teacher != "" {
			first = &schedules[0].Cells[index]
			break
		}
	}
	if first == nil {
		t.Fatal("expected at least one teacher-led lesson")
	}
	var second *ScheduleCell
	for index := range schedules[1].Cells {
		if schedules[1].Cells[index].Teacher != "" {
			second = &schedules[1].Cells[index]
			break
		}
	}
	if second == nil {
		t.Fatal("expected a second teacher-led lesson")
	}
	second.Day = first.Day
	second.Period = first.Period
	second.Teacher = first.Teacher
	report := ValidateSchedule(config, schedules)
	if report.TeacherConflicts == 0 || report.Passed {
		t.Fatalf("expected teacher conflict, got %+v", report)
	}
}

func TestConfigShapeUsesDynamicWeeklyCapacity(t *testing.T) {
	config := defaultConfig()
	config.TimeSlots = append(config.TimeSlots, TimeSlot{ID: "p10", Name: "第十节课", Start: "18:20", End: "19:00", Schedulable: true})
	config.Classes[0].Assignments[0].SingleLessons++
	if err := validateConfigShape(config); err != nil {
		t.Fatalf("46 lessons should fit a 50-cell week: %v", err)
	}
	config.Classes[0].Assignments[0].SingleLessons += 5
	if err := validateConfigShape(config); err == nil {
		t.Fatal("expected weekly lesson limit error above dynamic capacity")
	}
}

func TestSchedulingValidationUsesDynamicWeeklyCapacity(t *testing.T) {
	config := defaultConfig()
	config.TimeSlots = append(config.TimeSlots, TimeSlot{ID: "p10", Name: "第十节课", Start: "18:20", End: "19:00", Schedulable: true})
	for index := range config.Classes {
		config.Classes[index].Assignments[0].SingleLessons += 5
	}
	if err := validateForScheduling(config, schedulableSlots(config)); err != nil {
		t.Fatalf("dynamic 50-cell schedule should validate: %v", err)
	}
}

func TestBalancedDistributionPatternsRespectDoubleLessons(t *testing.T) {
	patterns := balancedDistributionPatterns(5, 4, 3)
	if len(patterns) == 0 {
		t.Fatal("expected balanced patterns for 4 singles and 3 double blocks")
	}
	for _, pattern := range patterns {
		minimum, maximum := 100, 0
		for day := 0; day < 5; day++ {
			sessions := pattern.Singles[day] + pattern.Blocks[day]
			lessons := pattern.Singles[day] + pattern.Blocks[day]*2
			if sessions < minimum {
				minimum = sessions
			}
			if sessions > maximum {
				maximum = sessions
			}
			if lessons > 2 {
				t.Fatalf("daily math lessons = %d, want at most 2: %+v", lessons, pattern)
			}
		}
		if maximum-minimum > 1 {
			t.Fatalf("session distribution is not balanced: %+v", pattern)
		}
	}
}

func TestValidationDetectsConcentratedCourseDistribution(t *testing.T) {
	config := defaultConfig()
	schedules, report, err := GenerateSchedule(context.Background(), config, nil)
	if err != nil || !report.Passed {
		t.Fatalf("generate balanced schedule: report=%+v err=%v", report, err)
	}
	schedule := &schedules[0]
	var physicsCells []ScheduleCell
	for _, cell := range schedule.Cells {
		if cell.SubjectID == "physics" && cell.BlockID == "" {
			physicsCells = append(physicsCells, cell)
		}
	}
	if len(physicsCells) < 2 {
		t.Fatal("expected physics lessons on multiple days")
	}
	source := physicsCells[0]
	targetDay := physicsCells[1].Day
	var target ScheduleCell
	for _, cell := range schedule.Cells {
		if cell.Day == targetDay && cell.SubjectID != "physics" && cell.BlockID == "" {
			target = cell
			break
		}
	}
	if target.SubjectID == "" {
		t.Fatal("expected a single lesson to swap on the target day")
	}
	err = swapScheduleRanges(config, schedules, schedule.ClassID,
		[]SchedulePosition{{Day: source.Day, Period: source.Period}},
		[]SchedulePosition{{Day: target.Day, Period: target.Period}},
	)
	if err != nil {
		t.Fatalf("create concentrated schedule: %v", err)
	}
	report = ValidateSchedule(config, schedules)
	if report.Passed || report.DistributionErrors == 0 {
		t.Fatalf("expected concentrated course distribution to fail: %+v", report)
	}
}
