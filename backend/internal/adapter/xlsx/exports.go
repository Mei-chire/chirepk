package xlsx

import "chirepk/backend/internal/domain"

type Config = domain.Config
type TimeSlot = domain.TimeSlot
type Subject = domain.Subject
type ClassConfig = domain.ClassConfig
type CourseAssignment = domain.CourseAssignment
type ScheduleRun = domain.ScheduleRun
type ClassSchedule = domain.ClassSchedule
type ScheduleCell = domain.ScheduleCell
type ValidationReport = domain.ValidationReport

func defaultConfig() Config {
	return domain.DefaultConfig()
}

func schedulableSlots(config Config) []domain.TimeSlot {
	return domain.SchedulableSlots(config)
}

func isTeacherlessSubject(subjectID string) bool {
	return domain.IsTeacherlessSubject(subjectID)
}

const MaxImportWorkbookSize = maxImportWorkbookSize

func ImportConfigFromXLSX(data []byte) (Config, error) {
	return importConfigFromXLSX(data)
}

func ParseLessonValue(raw string) (int, int, error) {
	return parseLessonValue(raw)
}

func BuildScheduleWorkbook(run ScheduleRun) ([]byte, error) {
	return buildScheduleWorkbook(run)
}

func ExportFileName(runName string) string {
	return exportFileName(runName)
}
