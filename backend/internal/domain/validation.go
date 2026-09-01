package domain

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SchedulableSlots returns only the time slots that can contain lessons.
func SchedulableSlots(config Config) []TimeSlot {
	slots := make([]TimeSlot, 0, len(config.TimeSlots))
	for _, slot := range config.TimeSlots {
		if slot.Schedulable {
			slots = append(slots, slot)
		}
	}
	return slots
}

// SubjectName resolves a subject id for user-facing validation messages.
func SubjectName(config Config, id string) string {
	for _, subject := range config.Subjects {
		if subject.ID == id {
			return subject.Name
		}
	}
	return id
}

// IsTeacherlessSubject identifies activities that do not require a teacher.
func IsTeacherlessSubject(subjectID string) bool {
	switch subjectID {
	case "club", "labor", "safety":
		return true
	default:
		return false
	}
}

// ValidateConfigShape validates the structural constraints shared by all use cases.
func ValidateConfigShape(config Config) error {
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
	previousEnd := -1
	for index, slot := range config.TimeSlots {
		if slot.ID == "" || slot.Name == "" || slot.Start == "" || slot.End == "" || slots[slot.ID] {
			return errors.New("作息信息存在空值或重复项")
		}
		start, err := clockMinutes(slot.Start)
		if err != nil {
			return fmt.Errorf("%s 的开始时间无效", slot.Name)
		}
		end, err := clockMinutes(slot.End)
		if err != nil {
			return fmt.Errorf("%s 的结束时间无效", slot.Name)
		}
		if end <= start {
			return fmt.Errorf("%s 的结束时间必须晚于开始时间", slot.Name)
		}
		if index > 0 && start < previousEnd {
			return fmt.Errorf("%s 与前一个时段发生时间重叠", slot.Name)
		}
		previousEnd = end
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

func clockMinutes(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, errors.New("invalid clock time")
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, errors.New("invalid clock time")
	}
	return hour*60 + minute, nil
}

// TeacherStats builds a deterministic workload summary from the current configuration.
func TeacherStats(config Config) []TeacherStat {
	type accumulator struct {
		lessons  int
		subjects map[string]bool
		classes  map[string]bool
	}
	byTeacher := make(map[string]*accumulator)
	subjectNames := make(map[string]string, len(config.Subjects))
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
