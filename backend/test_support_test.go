package main

import (
	"context"
	"encoding/json"

	"chirepk/backend/internal/adapter/xlsx"
	"chirepk/backend/internal/application"
	"chirepk/backend/internal/domain"
	"chirepk/backend/internal/scheduler"
	"chirepk/backend/internal/store"
	httpapi "chirepk/backend/internal/transport/http"
)

type Config = domain.Config
type TimeSlot = domain.TimeSlot
type CourseAssignment = domain.CourseAssignment
type ScheduleCell = domain.ScheduleCell
type ClassSchedule = domain.ClassSchedule
type ScheduleRun = domain.ScheduleRun
type SchedulePosition = domain.SchedulePosition
type ValidationReport = domain.ValidationReport
type adjustmentCandidatesResponse = scheduler.AdjustmentCandidatesResponse
type adjustmentCandidate = scheduler.AdjustmentCandidate

type adjustmentRequest struct {
	ClassID          string           `json:"classId"`
	Source           SchedulePosition `json:"source"`
	Target           SchedulePosition `json:"target"`
	ExpectedRevision int              `json:"expectedRevision"`
}

type undoAdjustmentRequest struct {
	ExpectedRevision int `json:"expectedRevision"`
}

var ErrRunRevisionConflict = store.ErrRunRevisionConflict

func defaultConfig() Config {
	return domain.DefaultConfig()
}

func NewMemoryStore() *store.MemoryStore {
	return store.NewMemoryStore()
}

type testAPI struct {
	*httpapi.API
	service *application.Service
}

func NewAPI(memoryStore *store.MemoryStore) *testAPI {
	service := application.NewService(memoryStore)
	return &testAPI{API: httpapi.NewAPI(service), service: service}
}

func (api *testAPI) preflightData() map[string]any {
	data, _ := json.Marshal(api.service.Preflight())
	var result map[string]any
	_ = json.Unmarshal(data, &result)
	return result
}

func teacherStats(config Config) []domain.TeacherStat {
	return domain.TeacherStats(config)
}

func importConfigFromXLSX(data []byte) (Config, error) {
	return xlsx.ImportConfigFromXLSX(data)
}

func parseLessonValue(raw string) (int, int, error) {
	return xlsx.ParseLessonValue(raw)
}

func buildScheduleWorkbook(run ScheduleRun) ([]byte, error) {
	return xlsx.BuildScheduleWorkbook(run)
}

func schedulableSlots(config Config) []TimeSlot {
	return domain.SchedulableSlots(config)
}

func validateForScheduling(config Config, slots []TimeSlot) error {
	return scheduler.ValidateForScheduling(config, slots)
}

func validateConfigShape(config Config) error {
	return domain.ValidateConfigShape(config)
}

func balancedDistributionPatterns(days, singles, blocks int) []scheduler.CourseDayPattern {
	return scheduler.BalancedDistributionPatterns(days, singles, blocks)
}

func GenerateSchedule(ctx context.Context, config Config, progress scheduler.ProgressFunc) ([]domain.ClassSchedule, ValidationReport, error) {
	return scheduler.GenerateSchedule(ctx, config, progress)
}

func ValidateSchedule(config Config, schedules []domain.ClassSchedule) ValidationReport {
	return scheduler.ValidateSchedule(config, schedules)
}

func buildAdjustmentCandidates(run ScheduleRun, classID string, source SchedulePosition) (adjustmentCandidatesResponse, error) {
	return scheduler.BuildAdjustmentCandidates(run, classID, source)
}

func prepareScheduleSwap(config Config, schedules []domain.ClassSchedule, classID string, source, target SchedulePosition) ([]SchedulePosition, []SchedulePosition, error) {
	return scheduler.PrepareScheduleSwap(config, schedules, classID, source, target)
}

func swapScheduleRanges(config Config, schedules []domain.ClassSchedule, classID string, source, target []SchedulePosition) error {
	return scheduler.SwapScheduleRanges(config, schedules, classID, source, target)
}

func clone[T any](value T) T {
	data, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(data, &result)
	return result
}
