package scheduler

import (
	"errors"
	"sort"
	"strconv"
)

type adjustmentCandidate struct {
	TargetPositions []SchedulePosition `json:"targetPositions"`
	TargetCells     []ScheduleCell     `json:"targetCells"`
}

type adjustmentCandidatesResponse struct {
	Revision        int                   `json:"revision"`
	SourcePositions []SchedulePosition    `json:"sourcePositions"`
	SourceCells     []ScheduleCell        `json:"sourceCells"`
	Candidates      []adjustmentCandidate `json:"candidates"`
}

func buildAdjustmentCandidates(run ScheduleRun, classID string, source SchedulePosition) (adjustmentCandidatesResponse, error) {
	schedule, err := findClassSchedule(run.Schedules, classID)
	if err != nil {
		return adjustmentCandidatesResponse{}, err
	}
	sourcePositions, err := scheduleSelection(*schedule, source)
	if err != nil {
		return adjustmentCandidatesResponse{}, err
	}
	response := adjustmentCandidatesResponse{
		Revision: run.Revision, SourcePositions: sourcePositions,
		SourceCells: cellsAt(*schedule, sourcePositions), Candidates: []adjustmentCandidate{},
	}
	slots := schedulableSlots(run.Config)
	for day := range run.Config.Days {
		periodLimit := len(slots)
		if len(sourcePositions) == 2 {
			periodLimit--
		}
		for period := 0; period < periodLimit; period++ {
			targetStart := SchedulePosition{Day: day, Period: period}
			targetPositions, targetErr := scheduleSelection(*schedule, targetStart)
			if targetErr != nil || len(targetPositions) != len(sourcePositions) {
				continue
			}
			if len(sourcePositions) == 2 && isMorning(slots[period]) != isMorning(slots[period+1]) {
				continue
			}
			if rangesOverlap(sourcePositions, targetPositions) {
				continue
			}
			trial := clone(run.Schedules)
			if err := swapScheduleRanges(run.Config, trial, classID, sourcePositions, targetPositions); err != nil {
				continue
			}
			if !ValidateSchedule(run.Config, trial).Passed {
				continue
			}
			response.Candidates = append(response.Candidates, adjustmentCandidate{
				TargetPositions: targetPositions, TargetCells: cellsAt(*schedule, targetPositions),
			})
		}
	}
	return response, nil
}

func prepareScheduleSwap(config Config, schedules []ClassSchedule, classID string, sourceStart, targetStart SchedulePosition) ([]SchedulePosition, []SchedulePosition, error) {
	schedule, err := findClassSchedule(schedules, classID)
	if err != nil {
		return nil, nil, err
	}
	source, err := scheduleSelection(*schedule, sourceStart)
	if err != nil {
		return nil, nil, err
	}
	target, err := scheduleSelection(*schedule, targetStart)
	if err != nil {
		return nil, nil, err
	}
	if len(source) != len(target) {
		return nil, nil, errors.New("连堂课必须整体交换")
	}
	if rangesOverlap(source, target) {
		return nil, nil, errors.New("请选择其他时段进行交换")
	}
	trial := clone(schedules)
	if err := swapScheduleRanges(config, trial, classID, source, target); err != nil {
		return nil, nil, err
	}
	if !ValidateSchedule(config, trial).Passed {
		return nil, nil, errors.New("该交换不满足教师、课时、连堂或第九节硬约束")
	}
	return source, target, nil
}

func scheduleSelection(schedule ClassSchedule, start SchedulePosition) ([]SchedulePosition, error) {
	cell, ok := cellAt(schedule, start)
	if !ok {
		return nil, errors.New("未找到所选课程")
	}
	if cell.BlockID == "" {
		return []SchedulePosition{start}, nil
	}
	positions := make([]SchedulePosition, 0, 2)
	for _, item := range schedule.Cells {
		if item.BlockID == cell.BlockID {
			positions = append(positions, SchedulePosition{Day: item.Day, Period: item.Period})
		}
	}
	if len(positions) != 2 {
		return nil, errors.New("所选连堂课结构不完整")
	}
	sortPositions(positions)
	return positions, nil
}

func swapScheduleRanges(config Config, schedules []ClassSchedule, classID string, source, target []SchedulePosition) error {
	if len(source) == 0 || len(source) != len(target) {
		return errors.New("交换位置数量不一致")
	}
	schedule, err := findClassSchedule(schedules, classID)
	if err != nil {
		return err
	}
	slots := schedulableSlots(config)
	sourceIndexes := make([]int, len(source))
	targetIndexes := make([]int, len(target))
	for index := range source {
		var ok bool
		sourceIndexes[index], ok = cellIndexAt(*schedule, source[index])
		if !ok {
			return errors.New("原课程位置不存在")
		}
		targetIndexes[index], ok = cellIndexAt(*schedule, target[index])
		if !ok {
			return errors.New("目标课程位置不存在")
		}
	}
	sourceCells := make([]ScheduleCell, len(source))
	targetCells := make([]ScheduleCell, len(target))
	for index := range source {
		sourceCells[index] = schedule.Cells[sourceIndexes[index]]
		targetCells[index] = schedule.Cells[targetIndexes[index]]
	}
	for index := range source {
		schedule.Cells[sourceIndexes[index]] = placeScheduleCell(targetCells[index], source[index], slots, classID)
		schedule.Cells[targetIndexes[index]] = placeScheduleCell(sourceCells[index], target[index], slots, classID)
	}
	return nil
}

func placeScheduleCell(cell ScheduleCell, position SchedulePosition, slots []TimeSlot, classID string) ScheduleCell {
	cell.Day = position.Day
	cell.Period = position.Period
	cell.ClassID = classID
	if position.Period >= 0 && position.Period < len(slots) {
		cell.SlotID = slots[position.Period].ID
	}
	return cell
}

func findClassSchedule(schedules []ClassSchedule, classID string) (*ClassSchedule, error) {
	for index := range schedules {
		if schedules[index].ClassID == classID {
			return &schedules[index], nil
		}
	}
	return nil, errors.New("班级课表不存在")
}

func cellsAt(schedule ClassSchedule, positions []SchedulePosition) []ScheduleCell {
	result := make([]ScheduleCell, 0, len(positions))
	for _, position := range positions {
		if cell, ok := cellAt(schedule, position); ok {
			result = append(result, cell)
		}
	}
	return result
}

func cellAt(schedule ClassSchedule, position SchedulePosition) (ScheduleCell, bool) {
	index, ok := cellIndexAt(schedule, position)
	if !ok {
		return ScheduleCell{}, false
	}
	return schedule.Cells[index], true
}

func cellIndexAt(schedule ClassSchedule, position SchedulePosition) (int, bool) {
	for index, cell := range schedule.Cells {
		if cell.Day == position.Day && cell.Period == position.Period {
			return index, true
		}
	}
	return 0, false
}

func rangesOverlap(left, right []SchedulePosition) bool {
	used := make(map[string]bool, len(left))
	for _, position := range left {
		used[positionKey(position)] = true
	}
	for _, position := range right {
		if used[positionKey(position)] {
			return true
		}
	}
	return false
}

func positionKey(position SchedulePosition) string {
	return strconv.Itoa(position.Day) + ":" + strconv.Itoa(position.Period)
}

func sortPositions(positions []SchedulePosition) {
	sort.Slice(positions, func(i, j int) bool {
		if positions[i].Day == positions[j].Day {
			return positions[i].Period < positions[j].Period
		}
		return positions[i].Day < positions[j].Day
	})
}
