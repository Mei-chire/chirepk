package store

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"chirepk/backend/internal/domain"
)

var ErrRunRevisionConflict = errors.New("课表已发生变化，请刷新后重试")

type MemoryStore struct {
	mu     sync.RWMutex
	config domain.Config
	setup  domain.SetupStatus
	runs   map[string]*domain.ScheduleRun
	order  []string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{config: domain.BlankConfig(), runs: make(map[string]*domain.ScheduleRun)}
}

func clone[T any](value T) T {
	data, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(data, &result)
	return result
}

func (s *MemoryStore) Config() domain.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.config)
}

func (s *MemoryStore) SetConfig(config domain.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config.UpdatedAt = time.Now()
	s.config = clone(config)
}

func (s *MemoryStore) Setup() domain.SetupStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.setup)
}

func (s *MemoryStore) ImportConfig(config domain.Config, sourceName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	config.UpdatedAt = now
	s.config = clone(config)
	s.setup = domain.SetupStatus{
		Imported:         true,
		AssignmentsReady: true,
		TimesReady:       true,
		SourceName:       sourceName,
		ImportedAt:       &now,
	}
	// A new source file describes a new configuration, so old schedules must
	// not remain selectable against it.
	s.runs = make(map[string]*domain.ScheduleRun)
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

func (s *MemoryStore) ResetConfig() domain.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = domain.BlankConfig()
	s.setup = domain.SetupStatus{}
	s.runs = make(map[string]*domain.ScheduleRun)
	s.order = nil
	return clone(s.config)
}

func (s *MemoryStore) AddRun(run *domain.ScheduleRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copyRun := clone(*run)
	s.runs[run.ID] = &copyRun
	s.order = append([]string{run.ID}, s.order...)
}

func (s *MemoryStore) UpdateRun(id string, update func(*domain.ScheduleRun)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return false
	}
	update(run)
	return true
}

func (s *MemoryStore) MutateRun(id string, expectedRevision int, mutate func(*domain.ScheduleRun) error) (domain.ScheduleRun, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return domain.ScheduleRun{}, false, nil
	}
	if run.Revision != expectedRevision {
		return domain.ScheduleRun{}, true, ErrRunRevisionConflict
	}
	working := clone(*run)
	if err := mutate(&working); err != nil {
		return domain.ScheduleRun{}, true, err
	}
	*run = working
	return clone(working), true, nil
}

func (s *MemoryStore) Run(id string) (domain.ScheduleRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return domain.ScheduleRun{}, false
	}
	return clone(*run), true
}

func (s *MemoryStore) Runs() []domain.ScheduleRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]domain.ScheduleRun, 0, len(s.order))
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
