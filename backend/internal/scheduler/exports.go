package scheduler

import "chirepk/backend/internal/domain"

type TimeSlot = domain.TimeSlot
type Subject = domain.Subject
type CourseAssignment = domain.CourseAssignment
type ClassConfig = domain.ClassConfig
type Config = domain.Config
type ScheduleCell = domain.ScheduleCell
type ClassSchedule = domain.ClassSchedule
type ValidationReport = domain.ValidationReport
type SchedulePosition = domain.SchedulePosition
type ScheduleAdjustment = domain.ScheduleAdjustment
type ScheduleRun = domain.ScheduleRun

func WeeklyCapacity(config Config) int {
	return domain.WeeklyCapacity(config)
}

func validateConfigShape(config Config) error {
	return domain.ValidateConfigShape(config)
}

func isTeacherlessSubject(subjectID string) bool {
	return domain.IsTeacherlessSubject(subjectID)
}

func schedulableSlots(config Config) []TimeSlot {
	return domain.SchedulableSlots(config)
}

func subjectName(config Config, id string) string {
	return domain.SubjectName(config, id)
}

// Public aliases keep the scheduler package usable by application services while
// leaving algorithm-specific helpers private to this package.
type ProgressFunc = progressFn
type AdjustmentCandidate = adjustmentCandidate
type AdjustmentCandidatesResponse = adjustmentCandidatesResponse
type CourseDayPattern = courseDayPattern

func SchedulableSlots(config Config) []TimeSlot {
	return schedulableSlots(config)
}

func ValidateForScheduling(config Config, slots []TimeSlot) error {
	return validateForScheduling(config, slots)
}

func BuildAdjustmentCandidates(run ScheduleRun, classID string, source SchedulePosition) (AdjustmentCandidatesResponse, error) {
	return buildAdjustmentCandidates(run, classID, source)
}

func PrepareScheduleSwap(config Config, schedules []ClassSchedule, classID string, sourceStart, targetStart SchedulePosition) ([]SchedulePosition, []SchedulePosition, error) {
	return prepareScheduleSwap(config, schedules, classID, sourceStart, targetStart)
}

func SwapScheduleRanges(config Config, schedules []ClassSchedule, classID string, source, target []SchedulePosition) error {
	return swapScheduleRanges(config, schedules, classID, source, target)
}

func BalancedDistributionPatterns(days, singles, blocks int) []CourseDayPattern {
	return balancedDistributionPatterns(days, singles, blocks)
}
