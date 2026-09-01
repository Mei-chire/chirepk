package domain

import "time"

type TimeSlot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Schedulable bool   `json:"schedulable"`
}

type Subject struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type CourseAssignment struct {
	SubjectID     string `json:"subjectId"`
	Teacher       string `json:"teacher"`
	SingleLessons int    `json:"singleLessons"`
	DoubleBlocks  int    `json:"doubleBlocks"`
}

func (a CourseAssignment) WeeklyLessons() int {
	return a.SingleLessons + a.DoubleBlocks*2
}

type ClassConfig struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Grade       string             `json:"grade"`
	HeadTeacher string             `json:"headTeacher"`
	Assignments []CourseAssignment `json:"assignments"`
}

type Config struct {
	Version    int           `json:"version"`
	SchoolName string        `json:"schoolName"`
	Semester   string        `json:"semester"`
	Days       []string      `json:"days"`
	TimeSlots  []TimeSlot    `json:"timeSlots"`
	Subjects   []Subject     `json:"subjects"`
	Classes    []ClassConfig `json:"classes"`
	UpdatedAt  time.Time     `json:"updatedAt"`
}

// WeeklyCapacity is the number of lesson cells available to each class in a
// week. Activity slots are excluded from the capacity.
func WeeklyCapacity(config Config) int {
	lessonSlots := 0
	for _, slot := range config.TimeSlots {
		if slot.Schedulable {
			lessonSlots++
		}
	}
	return len(config.Days) * lessonSlots
}

type ScheduleCell struct {
	Day       int    `json:"day"`
	Period    int    `json:"period"`
	SlotID    string `json:"slotId"`
	SubjectID string `json:"subjectId"`
	Teacher   string `json:"teacher"`
	ClassID   string `json:"classId"`
	IsDouble  bool   `json:"isDouble"`
	BlockID   string `json:"blockId,omitempty"`
}

type ClassSchedule struct {
	ClassID string         `json:"classId"`
	Cells   []ScheduleCell `json:"cells"`
}

type ValidationReport struct {
	Passed             bool     `json:"passed"`
	TeacherConflicts   int      `json:"teacherConflicts"`
	CountMismatches    int      `json:"countMismatches"`
	DoubleBlockErrors  int      `json:"doubleBlockErrors"`
	AfterServiceErrors int      `json:"afterServiceErrors"`
	DistributionErrors int      `json:"distributionErrors"`
	Messages           []string `json:"messages"`
}

type SchedulePosition struct {
	Day    int `json:"day"`
	Period int `json:"period"`
}

type ScheduleAdjustment struct {
	ID        string             `json:"id"`
	AppliedAt time.Time          `json:"appliedAt"`
	ClassID   string             `json:"classId"`
	Source    []SchedulePosition `json:"source"`
	Target    []SchedulePosition `json:"target"`
}

type ScheduleRun struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	CreatedAt   time.Time            `json:"createdAt"`
	FinishedAt  *time.Time           `json:"finishedAt,omitempty"`
	Status      string               `json:"status"`
	Progress    int                  `json:"progress"`
	Message     string               `json:"message"`
	Config      Config               `json:"config"`
	Schedules   []ClassSchedule      `json:"schedules,omitempty"`
	Validation  ValidationReport     `json:"validation"`
	Revision    int                  `json:"revision"`
	Adjustments []ScheduleAdjustment `json:"adjustments,omitempty"`
}

type TeacherStat struct {
	Teacher       string   `json:"teacher"`
	WeeklyLessons int      `json:"weeklyLessons"`
	Subjects      []string `json:"subjects"`
	Classes       []string `json:"classes"`
}
