package store

import "chirepk/backend/internal/domain"

// Repository is the persistence boundary used by application services.
// Implementations may be in-memory, SQL-backed, or remote without changing the API layer.
type Repository interface {
	Config() domain.Config
	SetConfig(domain.Config)
	Setup() domain.SetupStatus
	ImportConfig(domain.Config, string)
	MarkStage(string)
	ResetConfig() domain.Config
	AddRun(*domain.ScheduleRun)
	UpdateRun(string, func(*domain.ScheduleRun)) bool
	MutateRun(string, int, func(*domain.ScheduleRun) error) (domain.ScheduleRun, bool, error)
	Run(string) (domain.ScheduleRun, bool)
	Runs() []domain.ScheduleRun
	DeleteRun(string) bool
}

var _ Repository = (*MemoryStore)(nil)
