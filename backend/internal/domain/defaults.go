package domain

import (
	"fmt"
	"time"
)

// DefaultConfig is a complete, synthetic configuration used by scheduler
// tests and local demonstrations. It intentionally contains no real school
// or person data.
func DefaultConfig() Config {
	subjects := []Subject{
		{ID: "chinese", Name: "语文", Color: "#ff7a90"},
		{ID: "math", Name: "数学", Color: "#5b8def"},
		{ID: "english", Name: "英语", Color: "#9b72e7"},
		{ID: "morality", Name: "道法", Color: "#ef8f56"},
		{ID: "history", Name: "历史", Color: "#b17a55"},
		{ID: "geography", Name: "地理", Color: "#3ab49f"},
		{ID: "biology", Name: "生物", Color: "#6cad55"},
		{ID: "physics", Name: "物理", Color: "#4775c5"},
		{ID: "chemistry", Name: "化学", Color: "#8f70ad"},
		{ID: "music", Name: "音乐", Color: "#e96ca4"},
		{ID: "art", Name: "美术", Color: "#f2a93b"},
		{ID: "pe", Name: "体育", Color: "#31a7c7"},
		{ID: "club", Name: "社团", Color: "#e66f5b"},
		{ID: "labor", Name: "劳技", Color: "#7f9c47"},
		{ID: "safety", Name: "安全", Color: "#79808e"},
	}
	hours := map[string][2]int{
		"chinese": {6, 0}, "math": {4, 3}, "english": {4, 2},
		"morality": {2, 0}, "history": {2, 0}, "geography": {3, 0},
		"biology": {3, 0}, "physics": {5, 0}, "chemistry": {0, 0},
		"music": {1, 0}, "art": {1, 0}, "pe": {1, 0}, "club": {2, 0},
		"labor": {1, 0}, "safety": {0, 0},
	}
	classes := make([]ClassConfig, 0, 21)
	for classIndex := 1; classIndex <= 21; classIndex++ {
		assignments := make([]CourseAssignment, 0, len(subjects))
		for _, subject := range subjects {
			hourSpec := hours[subject.ID]
			teacher := ""
			if hourSpec[0] > 0 || hourSpec[1] > 0 {
				// Each synthetic class has its own teacher set so the demo is
				// deterministic and does not model any real staffing roster.
				teacher = fmt.Sprintf("示例教师%02d-%s", classIndex, subject.ID)
			}
			assignments = append(assignments, CourseAssignment{
				SubjectID: subject.ID, Teacher: teacher,
				SingleLessons: hourSpec[0], DoubleBlocks: hourSpec[1],
			})
		}
		classes = append(classes, ClassConfig{
			ID:          fmt.Sprintf("sample-%02d", classIndex),
			Name:        fmt.Sprintf("示例班级 %02d", classIndex),
			Grade:       "示例年级",
			HeadTeacher: fmt.Sprintf("示例班主任%02d", classIndex),
			Assignments: assignments,
		})
	}
	return Config{
		Version: 1, SchoolName: "示例学校", Semester: "示例学期",
		Days: []string{"星期一", "星期二", "星期三", "星期四", "星期五"},
		TimeSlots: []TimeSlot{
			{ID: "p1", Name: "第一节课", Start: "08:00", End: "08:40", Schedulable: true},
			{ID: "break", Name: "大课间", Start: "08:40", End: "09:10", Schedulable: false},
			{ID: "p2", Name: "第二节课", Start: "09:10", End: "09:50", Schedulable: true},
			{ID: "p3", Name: "第三节课", Start: "10:05", End: "10:45", Schedulable: true},
			{ID: "eye-am", Name: "眼保健操", Start: "10:45", End: "11:00", Schedulable: false},
			{ID: "p4", Name: "第四节课", Start: "11:00", End: "11:40", Schedulable: true},
			{ID: "lunch", Name: "中餐", Start: "11:40", End: "12:00", Schedulable: false},
			{ID: "rest", Name: "午休", Start: "12:40", End: "14:00", Schedulable: false},
			{ID: "p5", Name: "第五节课", Start: "14:20", End: "15:00", Schedulable: true},
			{ID: "p6", Name: "第六节课", Start: "15:10", End: "15:50", Schedulable: true},
			{ID: "eye-pm", Name: "眼保健操", Start: "15:50", End: "16:05", Schedulable: false},
			{ID: "p7", Name: "第七节课", Start: "16:05", End: "16:45", Schedulable: true},
			{ID: "p8", Name: "课后服务1", Start: "16:55", End: "17:30", Schedulable: true},
			{ID: "p9", Name: "课后服务2", Start: "17:40", End: "18:10", Schedulable: true},
		},
		Subjects: subjects, Classes: classes, UpdatedAt: time.Now(),
	}
}

// BlankConfig keeps reusable timetable and subject definitions while waiting
// for the user to import the authoritative teacher workbook.
func BlankConfig() Config {
	config := DefaultConfig()
	for index := range config.Classes {
		config.Classes[index].HeadTeacher = ""
		config.Classes[index].Assignments = make([]CourseAssignment, 0)
	}
	return config
}
