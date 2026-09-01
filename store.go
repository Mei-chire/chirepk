package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu     sync.RWMutex
	config Config
	setup  SetupStatus
	runs   map[string]*ScheduleRun
	order  []string
}

type SetupStatus struct {
	Imported         bool       `json:"imported"`
	AssignmentsReady bool       `json:"assignmentsReady"`
	TimesReady       bool       `json:"timesReady"`
	SourceName       string     `json:"sourceName,omitempty"`
	ImportedAt       *time.Time `json:"importedAt,omitempty"`
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{config: blankConfig(), runs: make(map[string]*ScheduleRun)}
}

func clone[T any](value T) T {
	data, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(data, &result)
	return result
}

func (s *MemoryStore) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.config)
}

func (s *MemoryStore) SetConfig(config Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config.UpdatedAt = time.Now()
	s.config = clone(config)
}

func (s *MemoryStore) Setup() SetupStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.setup)
}

func (s *MemoryStore) ImportConfig(config Config, sourceName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	config.UpdatedAt = now
	s.config = clone(config)
	s.setup = SetupStatus{Imported: true, SourceName: sourceName, ImportedAt: &now}
	// A new source file describes a new configuration, so old schedules must
	// not remain selectable against it.
	s.runs = make(map[string]*ScheduleRun)
	s.order = nil
}

func (s *MemoryStore) MarkStage(stage string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.setup.Imported {
		return
	}
	switch stage {
	case "assignments":
		s.setup.AssignmentsReady = true
	case "times":
		s.setup.TimesReady = true
	}
}

func (s *MemoryStore) ResetConfig() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = blankConfig()
	s.setup = SetupStatus{}
	s.runs = make(map[string]*ScheduleRun)
	s.order = nil
	return clone(s.config)
}

func (s *MemoryStore) AddRun(run *ScheduleRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copyRun := clone(*run)
	s.runs[run.ID] = &copyRun
	s.order = append([]string{run.ID}, s.order...)
}

func (s *MemoryStore) UpdateRun(id string, update func(*ScheduleRun)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return false
	}
	update(run)
	return true
}

func (s *MemoryStore) Run(id string) (ScheduleRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return ScheduleRun{}, false
	}
	return clone(*run), true
}

func (s *MemoryStore) Runs() []ScheduleRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]ScheduleRun, 0, len(s.order))
	for _, id := range s.order {
		if run, ok := s.runs[id]; ok {
			runs = append(runs, clone(*run))
		}
	}
	return runs
}

func (s *MemoryStore) DeleteRun(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[id]; !ok {
		return false
	}
	delete(s.runs, id)
	for i, current := range s.order {
		if current == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	return true
}

func teacherStats(config Config) []TeacherStat {
	type accumulator struct {
		lessons  int
		subjects map[string]bool
		classes  map[string]bool
	}
	byTeacher := make(map[string]*accumulator)
	subjectNames := make(map[string]string)
	for _, subject := range config.Subjects {
		subjectNames[subject.ID] = subject.Name
	}
	for _, class := range config.Classes {
		for _, assignment := range class.Assignments {
			if assignment.Teacher == "" || assignment.WeeklyLessons() == 0 {
				continue
			}
			item := byTeacher[assignment.Teacher]
			if item == nil {
				item = &accumulator{subjects: make(map[string]bool), classes: make(map[string]bool)}
				byTeacher[assignment.Teacher] = item
			}
			item.lessons += assignment.WeeklyLessons()
			item.subjects[subjectNames[assignment.SubjectID]] = true
			item.classes[class.Name] = true
		}
	}
	stats := make([]TeacherStat, 0, len(byTeacher))
	for teacher, item := range byTeacher {
		stat := TeacherStat{Teacher: teacher, WeeklyLessons: item.lessons}
		for subject := range item.subjects {
			stat.Subjects = append(stat.Subjects, subject)
		}
		for class := range item.classes {
			stat.Classes = append(stat.Classes, class)
		}
		sort.Strings(stat.Subjects)
		sort.Strings(stat.Classes)
		stats = append(stats, stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].WeeklyLessons == stats[j].WeeklyLessons {
			return stats[i].Teacher < stats[j].Teacher
		}
		return stats[i].WeeklyLessons > stats[j].WeeklyLessons
	})
	return stats
}

func validateConfigShape(config Config) error {
	if len(config.Days) != 5 {
		return errors.New("当前版本固定为每周五天")
	}
	if len(config.Subjects) == 0 || len(config.Classes) == 0 {
		return errors.New("学科和班级不能为空")
	}
	subjects := make(map[string]bool)
	for _, subject := range config.Subjects {
		if subject.ID == "" || subject.Name == "" || subjects[subject.ID] {
			return errors.New("学科信息存在空值或重复项")
		}
		subjects[subject.ID] = true
	}
	slots := make(map[string]bool)
	for _, slot := range config.TimeSlots {
		if slot.ID == "" || slot.Name == "" || slot.Start == "" || slot.End == "" || slots[slot.ID] {
			return errors.New("作息信息存在空值或重复项")
		}
		slots[slot.ID] = true
	}
	capacity := WeeklyCapacity(config)
	for _, class := range config.Classes {
		if class.ID == "" || class.Name == "" {
			return errors.New("班级信息不能为空")
		}
		seen := make(map[string]bool)
		weeklyLessons := 0
		for _, assignment := range class.Assignments {
			if !subjects[assignment.SubjectID] || seen[assignment.SubjectID] {
				return errors.New(class.Name + " 的任课信息存在未知或重复学科")
			}
			if assignment.SingleLessons < 0 || assignment.DoubleBlocks < 0 {
				return errors.New(class.Name + " 的课时数不能小于零")
			}
			seen[assignment.SubjectID] = true
			weeklyLessons += assignment.WeeklyLessons()
		}
		if weeklyLessons > capacity {
			return fmt.Errorf("%s 的周总课时不能超过 %d", class.Name, capacity)
		}
	}
	return nil
}
