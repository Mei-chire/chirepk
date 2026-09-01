package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"
)

type progressFn func(int, string)

type teacherMask struct {
	teacher string
	mask    uint64
}

type scheduleCandidate struct {
	cells        []ScheduleCell
	teacherMasks []teacherMask
	quality      int
}

type slotPair struct {
	first  int
	second int
}

var afterServiceQuota = map[string]int{
	"math": 2, "chinese": 1, "english": 1, "physics": 1,
}

func GenerateSchedule(ctx context.Context, config Config, progress progressFn) ([]ClassSchedule, ValidationReport, error) {
	if progress == nil {
		progress = func(int, string) {}
	}
	slots := schedulableSlots(config)
	if err := validateForScheduling(config, slots); err != nil {
		return nil, ValidationReport{}, err
	}
	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))
	progress(4, "正在检查课时容量")

	const candidatesPerClass = 320
	for attempt := 0; attempt < 5; attempt++ {
		all := make([][]scheduleCandidate, len(config.Classes))
		for i, class := range config.Classes {
			if err := ctx.Err(); err != nil {
				return nil, ValidationReport{}, err
			}
			candidates := make([]scheduleCandidate, 0, candidatesPerClass)
			for len(candidates) < candidatesPerClass {
				candidate, err := generateCandidate(config, class, slots, rng)
				if err != nil {
					return nil, ValidationReport{}, err
				}
				candidates = append(candidates, candidate)
			}
			sort.SliceStable(candidates, func(a, b int) bool { return candidates[a].quality < candidates[b].quality })
			all[i] = candidates
			percent := 8 + (i+1)*30/len(config.Classes)
			progress(percent, "正在生成 "+class.Name+" 的候选课表")
		}

		selected := make([]int, len(config.Classes))
		for i := range selected {
			selected[i] = -1
		}
		occupancy := make(map[string]uint64)
		remaining := make([]int, len(config.Classes))
		for i := range remaining {
			remaining[i] = i
		}
		maxDepth := 0
		nodes := 0
		var solve func([]int, int) bool
		solve = func(left []int, depth int) bool {
			if ctx.Err() != nil || nodes > 2_500_000 {
				return false
			}
			if len(left) == 0 {
				return true
			}
			nodes++
			bestPosition := -1
			bestValid := make([]int, 0)
			bestCount := int(^uint(0) >> 1)
			for position, classIndex := range left {
				valid := make([]int, 0, 64)
				for candidateIndex := range all[classIndex] {
					if !candidateConflicts(all[classIndex][candidateIndex], occupancy) {
						valid = append(valid, candidateIndex)
					}
				}
				if len(valid) < bestCount {
					bestCount = len(valid)
					bestPosition = position
					bestValid = valid
				}
				if bestCount == 0 {
					break
				}
			}
			if bestCount == 0 {
				return false
			}
			classIndex := left[bestPosition]
			next := append([]int(nil), left[:bestPosition]...)
			next = append(next, left[bestPosition+1:]...)
			limit := len(bestValid)
			if limit > 100 {
				limit = 100
			}
			for _, candidateIndex := range bestValid[:limit] {
				candidate := all[classIndex][candidateIndex]
				applyCandidate(candidate, occupancy, true)
				selected[classIndex] = candidateIndex
				if depth+1 > maxDepth {
					maxDepth = depth + 1
					progress(40+maxDepth*50/len(config.Classes), "正在协调教师时间 · 已完成 "+strconv.Itoa(maxDepth)+"/"+strconv.Itoa(len(config.Classes))+" 个班")
				}
				if solve(next, depth+1) {
					return true
				}
				selected[classIndex] = -1
				applyCandidate(candidate, occupancy, false)
			}
			return false
		}
		if solve(remaining, 0) {
			schedules := make([]ClassSchedule, len(config.Classes))
			for classIndex, candidateIndex := range selected {
				cells := all[classIndex][candidateIndex].cells
				sort.Slice(cells, func(i, j int) bool {
					if cells[i].Day == cells[j].Day {
						return cells[i].Period < cells[j].Period
					}
					return cells[i].Day < cells[j].Day
				})
				schedules[classIndex] = ClassSchedule{ClassID: config.Classes[classIndex].ID, Cells: cells}
			}
			progress(94, "正在执行排课结果自检")
			report := ValidateSchedule(config, schedules)
			if !report.Passed {
				return nil, report, errors.New("排课结果未通过自检")
			}
			progress(100, "排课完成，全部约束已通过")
			return schedules, report, nil
		}
		progress(40, "正在扩充候选方案并重试")
	}
	return nil, ValidationReport{}, errors.New("当前条件较紧，未能在本次计算中找到无冲突方案，请稍后重试")
}

func schedulableSlots(config Config) []TimeSlot {
	var slots []TimeSlot
	for _, slot := range config.TimeSlots {
		if slot.Schedulable {
			slots = append(slots, slot)
		}
	}
	return slots
}

func validateForScheduling(config Config, slots []TimeSlot) error {
	if err := validateConfigShape(config); err != nil {
		return err
	}
	if len(slots) == 0 {
		return errors.New("每日作息至少需要一个可排课时段")
	}
	capacity := WeeklyCapacity(config)
	for _, class := range config.Classes {
		total := 0
		assignments := make(map[string]CourseAssignment)
		doubleBlocks := 0
		for _, assignment := range class.Assignments {
			total += assignment.WeeklyLessons()
			doubleBlocks += assignment.DoubleBlocks
			assignments[assignment.SubjectID] = assignment
			if assignment.WeeklyLessons() > 0 && strings.TrimSpace(assignment.Teacher) == "" && !isTeacherlessSubject(assignment.SubjectID) {
				return fmt.Errorf("%s 的 %s 尚未设置教师", class.Name, subjectName(config, assignment.SubjectID))
			}
		}
		if total != capacity {
			return fmt.Errorf("%s 周课时为 %d，需与可排容量 %d 一致", class.Name, total, capacity)
		}
		for subjectID, required := range afterServiceQuota {
			if assignments[subjectID].SingleLessons < required {
				return fmt.Errorf("%s 的%s普通课时少于第九节所需的 %d 节", class.Name, subjectName(config, subjectID), required)
			}
		}
		if doubleBlocks > 20 {
			return fmt.Errorf("%s 的连堂课块过多", class.Name)
		}
	}
	return nil
}

func generateCandidate(config Config, class ClassConfig, slots []TimeSlot, rng *rand.Rand) (scheduleCandidate, error) {
	periods := len(slots)
	totalPositions := len(config.Days) * periods
	cells := make([]ScheduleCell, totalPositions)
	occupied := make([]bool, totalPositions)

	var doubleSubjects []string
	var singleSubjects []string
	teachers := make(map[string]string)
	for _, assignment := range class.Assignments {
		teachers[assignment.SubjectID] = assignment.Teacher
		for i := 0; i < assignment.DoubleBlocks; i++ {
			doubleSubjects = append(doubleSubjects, assignment.SubjectID)
		}
		remainingSingles := assignment.SingleLessons - afterServiceQuota[assignment.SubjectID]
		for i := 0; i < remainingSingles; i++ {
			singleSubjects = append(singleSubjects, assignment.SubjectID)
		}
	}

	allowed := allowedDoublePairs(config, slots)
	pairs, ok := selectPairs(allowed, len(doubleSubjects), rng)
	if !ok {
		return scheduleCandidate{}, errors.New(class.Name + " 无法放置全部连堂课")
	}
	rng.Shuffle(len(doubleSubjects), func(i, j int) { doubleSubjects[i], doubleSubjects[j] = doubleSubjects[j], doubleSubjects[i] })
	for i, pair := range pairs {
		subjectID := doubleSubjects[i]
		blockID := class.ID + "-" + strconv.Itoa(i+1)
		for _, position := range []int{pair.first, pair.second} {
			day, period := position/periods, position%periods
			cells[position] = ScheduleCell{Day: day, Period: period, SlotID: slots[period].ID, SubjectID: subjectID, Teacher: teachers[subjectID], ClassID: class.ID, IsDouble: true, BlockID: blockID}
			occupied[position] = true
		}
	}

	afterSubjects := make([]string, 0, len(config.Days))
	for subjectID, count := range afterServiceQuota {
		for i := 0; i < count; i++ {
			afterSubjects = append(afterSubjects, subjectID)
		}
	}
	rng.Shuffle(len(afterSubjects), func(i, j int) { afterSubjects[i], afterSubjects[j] = afterSubjects[j], afterSubjects[i] })
	for day, subjectID := range afterSubjects {
		position := day*periods + periods - 1
		cells[position] = ScheduleCell{Day: day, Period: periods - 1, SlotID: slots[periods-1].ID, SubjectID: subjectID, Teacher: teachers[subjectID], ClassID: class.ID}
		occupied[position] = true
	}

	open := make([]int, 0, len(singleSubjects))
	for position := 0; position < totalPositions; position++ {
		if !occupied[position] {
			open = append(open, position)
		}
	}
	if len(open) != len(singleSubjects) {
		return scheduleCandidate{}, fmt.Errorf("%s 的课时结构与可排时段不匹配", class.Name)
	}
	rng.Shuffle(len(singleSubjects), func(i, j int) { singleSubjects[i], singleSubjects[j] = singleSubjects[j], singleSubjects[i] })
	for i, position := range open {
		subjectID := singleSubjects[i]
		day, period := position/periods, position%periods
		cells[position] = ScheduleCell{Day: day, Period: period, SlotID: slots[period].ID, SubjectID: subjectID, Teacher: teachers[subjectID], ClassID: class.ID}
	}

	candidate := scheduleCandidate{cells: cells}
	candidate.quality = candidateQuality(cells, periods)
	masks := make(map[string]uint64)
	for position, cell := range cells {
		if cell.Teacher != "" {
			masks[cell.Teacher] |= uint64(1) << position
		}
	}
	for teacher, mask := range masks {
		candidate.teacherMasks = append(candidate.teacherMasks, teacherMask{teacher: teacher, mask: mask})
	}
	return candidate, nil
}

func allowedDoublePairs(config Config, slots []TimeSlot) []slotPair {
	periods := len(slots)
	var pairs []slotPair
	for day := range config.Days {
		for period := 0; period < periods-2; period++ {
			if isMorning(slots[period]) != isMorning(slots[period+1]) {
				continue
			}
			first := day*periods + period
			pairs = append(pairs, slotPair{first: first, second: first + 1})
		}
	}
	return pairs
}

func selectPairs(allowed []slotPair, count int, rng *rand.Rand) ([]slotPair, bool) {
	if count == 0 {
		return nil, true
	}
	for attempt := 0; attempt < 100; attempt++ {
		shuffled := append([]slotPair(nil), allowed...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		used := make(map[int]bool)
		selected := make([]slotPair, 0, count)
		for _, pair := range shuffled {
			if used[pair.first] || used[pair.second] {
				continue
			}
			selected = append(selected, pair)
			used[pair.first], used[pair.second] = true, true
			if len(selected) == count {
				return selected, true
			}
		}
	}
	return nil, false
}

func candidateQuality(cells []ScheduleCell, periods int) int {
	quality := 0
	for day := 0; day < len(cells)/periods; day++ {
		counts := make(map[string]int)
		for period := 0; period < periods; period++ {
			cell := cells[day*periods+period]
			counts[cell.SubjectID]++
			if period > 0 {
				previous := cells[day*periods+period-1]
				if previous.SubjectID == cell.SubjectID && (cell.BlockID == "" || previous.BlockID != cell.BlockID) {
					quality += 18
				}
			}
		}
		for _, count := range counts {
			if count > 2 {
				quality += (count - 2) * 5
			}
		}
	}
	return quality
}

func candidateConflicts(candidate scheduleCandidate, occupancy map[string]uint64) bool {
	for _, item := range candidate.teacherMasks {
		if occupancy[item.teacher]&item.mask != 0 {
			return true
		}
	}
	return false
}

func applyCandidate(candidate scheduleCandidate, occupancy map[string]uint64, add bool) {
	for _, item := range candidate.teacherMasks {
		if add {
			occupancy[item.teacher] |= item.mask
		} else {
			occupancy[item.teacher] &^= item.mask
		}
	}
}

func isMorning(slot TimeSlot) bool {
	parts := strings.Split(slot.Start, ":")
	if len(parts) != 2 {
		return true
	}
	hour, _ := strconv.Atoi(parts[0])
	return hour < 12
}

func subjectName(config Config, id string) string {
	for _, subject := range config.Subjects {
		if subject.ID == id {
			return subject.Name
		}
	}
	return id
}

func ValidateSchedule(config Config, schedules []ClassSchedule) ValidationReport {
	report := ValidationReport{Passed: true}
	slots := schedulableSlots(config)
	periods := len(slots)
	teacherUse := make(map[string]ScheduleCell)
	classByID := make(map[string]ClassConfig)
	for _, class := range config.Classes {
		classByID[class.ID] = class
	}
	for _, schedule := range schedules {
		class, ok := classByID[schedule.ClassID]
		if !ok {
			report.CountMismatches++
			continue
		}
		counts := make(map[string]int)
		blocks := make(map[string][]ScheduleCell)
		afterCounts := make(map[string]int)
		for _, cell := range schedule.Cells {
			counts[cell.SubjectID]++
			if cell.BlockID != "" {
				blocks[cell.BlockID] = append(blocks[cell.BlockID], cell)
			}
			if cell.Period == periods-1 {
				afterCounts[cell.SubjectID]++
			}
			key := cell.Teacher + ":" + strconv.Itoa(cell.Day) + ":" + strconv.Itoa(cell.Period)
			if cell.Teacher != "" {
				if previous, exists := teacherUse[key]; exists && previous.ClassID != cell.ClassID {
					report.TeacherConflicts++
				} else {
					teacherUse[key] = cell
				}
			}
		}
		for _, assignment := range class.Assignments {
			if counts[assignment.SubjectID] != assignment.WeeklyLessons() {
				report.CountMismatches++
			}
			blockCount := 0
			for _, blockCells := range blocks {
				if len(blockCells) > 0 && blockCells[0].SubjectID == assignment.SubjectID {
					blockCount++
				}
			}
			if blockCount != assignment.DoubleBlocks {
				report.DoubleBlockErrors++
			}
		}
		for _, blockCells := range blocks {
			if len(blockCells) != 2 || blockCells[0].Day != blockCells[1].Day || abs(blockCells[0].Period-blockCells[1].Period) != 1 {
				report.DoubleBlockErrors++
				continue
			}
			if isMorning(slots[blockCells[0].Period]) != isMorning(slots[blockCells[1].Period]) {
				report.DoubleBlockErrors++
			}
		}
		for subjectID, expected := range afterServiceQuota {
			if afterCounts[subjectID] != expected {
				report.AfterServiceErrors++
			}
		}
		if len(schedule.Cells) != len(config.Days)*periods {
			report.CountMismatches++
		}
	}
	report.Passed = report.TeacherConflicts == 0 && report.CountMismatches == 0 && report.DoubleBlockErrors == 0 && report.AfterServiceErrors == 0 && len(schedules) == len(config.Classes)
	if report.Passed {
		report.Messages = []string{
			"教师时间冲突 0 项",
			"班级课时数量全部匹配",
			"连堂课均位于同一半天",
			"第九节专项课时全部匹配",
		}
	} else {
		report.Messages = []string{fmt.Sprintf("教师冲突 %d，课时错误 %d，连堂错误 %d，第九节错误 %d", report.TeacherConflicts, report.CountMismatches, report.DoubleBlockErrors, report.AfterServiceErrors)}
	}
	return report
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
