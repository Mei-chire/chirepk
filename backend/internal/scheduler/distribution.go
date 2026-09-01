package scheduler

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
)

type courseDayPattern struct {
	Singles []int
	Blocks  []int
}

type assignmentPatternSet struct {
	SubjectID string
	Patterns  []courseDayPattern
}

type distributionPatternKey struct {
	Days    int
	Singles int
	Blocks  int
}

var distributionPatternCache sync.Map

func balancedDayPlan(config Config, class ClassConfig, slots []TimeSlot, rng *rand.Rand) (map[string]courseDayPattern, []string, error) {
	days := len(config.Days)
	periods := len(slots)
	sets := make([]assignmentPatternSet, 0, len(class.Assignments))
	for _, assignment := range class.Assignments {
		if assignment.WeeklyLessons() == 0 {
			continue
		}
		patterns := balancedDistributionPatterns(days, assignment.SingleLessons, assignment.DoubleBlocks)
		if len(patterns) == 0 {
			return nil, nil, fmt.Errorf("%s 的%s无法按五天均匀分布", class.Name, subjectName(config, assignment.SubjectID))
		}
		sets = append(sets, assignmentPatternSet{SubjectID: assignment.SubjectID, Patterns: patterns})
	}
	sort.SliceStable(sets, func(i, j int) bool { return len(sets[i].Patterns) < len(sets[j].Patterns) })

	loads := make([]int, days)
	blockLoads := make([]int, days)
	maxBlocks := maxDoubleBlocksPerDay(slots)
	chosen := make(map[string]courseDayPattern, len(sets))
	var afterByDay []string
	var choose func(int) bool
	choose = func(index int) bool {
		if index == len(sets) {
			var ok bool
			afterByDay, ok = allocateAfterServiceDays(chosen, days, rng)
			return ok
		}
		set := sets[index]
		for _, patternIndex := range rng.Perm(len(set.Patterns)) {
			pattern := set.Patterns[patternIndex]
			fits := true
			for day := 0; day < days; day++ {
				lessons := pattern.Singles[day] + pattern.Blocks[day]*2
				if loads[day]+lessons > periods || blockLoads[day]+pattern.Blocks[day] > maxBlocks {
					fits = false
					break
				}
			}
			if !fits {
				continue
			}
			chosen[set.SubjectID] = pattern
			for day := 0; day < days; day++ {
				loads[day] += pattern.Singles[day] + pattern.Blocks[day]*2
				blockLoads[day] += pattern.Blocks[day]
			}
			if choose(index + 1) {
				return true
			}
			for day := 0; day < days; day++ {
				loads[day] -= pattern.Singles[day] + pattern.Blocks[day]*2
				blockLoads[day] -= pattern.Blocks[day]
			}
			delete(chosen, set.SubjectID)
		}
		return false
	}
	if !choose(0) {
		return nil, nil, errors.New(class.Name + " 无法在每日容量内均匀分布全部课程")
	}
	return chosen, afterByDay, nil
}

func balancedDistributionPatterns(days, singles, blocks int) []courseDayPattern {
	key := distributionPatternKey{Days: days, Singles: singles, Blocks: blocks}
	if cached, ok := distributionPatternCache.Load(key); ok {
		return cached.([]courseDayPattern)
	}
	if days <= 0 || singles < 0 || blocks < 0 {
		return nil
	}
	units := singles + blocks
	base := units / days
	highDays := units % days
	cap := dailyLessonCap(days, singles+blocks*2, blocks)
	var patterns []courseDayPattern
	sessionCounts := make([]int, days)
	var chooseHighDays func(int, int)
	chooseHighDays = func(day, remaining int) {
		if day == days {
			if remaining != 0 {
				return
			}
			blockCounts := make([]int, days)
			var assignBlocks func(int, int)
			assignBlocks = func(blockDay, remainingBlocks int) {
				if blockDay == days {
					if remainingBlocks != 0 {
						return
					}
					singleCounts := make([]int, days)
					for index := range singleCounts {
						singleCounts[index] = sessionCounts[index] - blockCounts[index]
					}
					patterns = append(patterns, courseDayPattern{Singles: append([]int(nil), singleCounts...), Blocks: append([]int(nil), blockCounts...)})
					return
				}
				maxBlocks := sessionCounts[blockDay]
				if maxBlocks > remainingBlocks {
					maxBlocks = remainingBlocks
				}
				for count := 0; count <= maxBlocks; count++ {
					if sessionCounts[blockDay]+count > cap {
						continue
					}
					blockCounts[blockDay] = count
					assignBlocks(blockDay+1, remainingBlocks-count)
				}
			}
			assignBlocks(0, blocks)
			return
		}
		sessionCounts[day] = base
		chooseHighDays(day+1, remaining)
		if remaining > 0 {
			sessionCounts[day] = base + 1
			chooseHighDays(day+1, remaining-1)
		}
	}
	chooseHighDays(0, highDays)
	distributionPatternCache.Store(key, patterns)
	return patterns
}

func allocateAfterServiceDays(plans map[string]courseDayPattern, days int, rng *rand.Rand) ([]string, bool) {
	var subjects []string
	for subjectID, count := range afterServiceQuota {
		for index := 0; index < count; index++ {
			subjects = append(subjects, subjectID)
		}
	}
	if len(subjects) != days {
		return nil, false
	}
	rng.Shuffle(len(subjects), func(i, j int) { subjects[i], subjects[j] = subjects[j], subjects[i] })
	result := make([]string, days)
	used := make([]bool, len(subjects))
	var assign func(int) bool
	assign = func(day int) bool {
		if day == days {
			return true
		}
		for _, subjectIndex := range rng.Perm(len(subjects)) {
			if used[subjectIndex] {
				continue
			}
			subjectID := subjects[subjectIndex]
			plan, ok := plans[subjectID]
			if !ok || plan.Singles[day] == 0 {
				continue
			}
			used[subjectIndex] = true
			result[day] = subjectID
			if assign(day + 1) {
				return true
			}
			used[subjectIndex] = false
		}
		return false
	}
	return result, assign(0)
}

func dailyLessonCap(days, weeklyLessons, doubleBlocks int) int {
	if days <= 0 {
		return 0
	}
	cap := (weeklyLessons + days - 1) / days
	if doubleBlocks > 0 && cap < 2 {
		cap = 2
	}
	return cap
}

func maxDoubleBlocksPerDay(slots []TimeSlot) int {
	used := make([]bool, len(slots))
	count := 0
	for period := 0; period < len(slots)-2; period++ {
		if used[period] || used[period+1] || isMorning(slots[period]) != isMorning(slots[period+1]) {
			continue
		}
		used[period], used[period+1] = true, true
		count++
	}
	return count
}

func courseDistributionErrors(config Config, class ClassConfig, schedule ClassSchedule) int {
	days := len(config.Days)
	if days == 0 {
		return 1
	}
	dailyLessons := make(map[string][]int)
	dailySessions := make(map[string][]int)
	seenBlocks := make(map[string]bool)
	for _, cell := range schedule.Cells {
		if cell.Day < 0 || cell.Day >= days {
			continue
		}
		if dailyLessons[cell.SubjectID] == nil {
			dailyLessons[cell.SubjectID] = make([]int, days)
			dailySessions[cell.SubjectID] = make([]int, days)
		}
		dailyLessons[cell.SubjectID][cell.Day]++
		if cell.BlockID == "" {
			dailySessions[cell.SubjectID][cell.Day]++
		} else if !seenBlocks[cell.BlockID] {
			seenBlocks[cell.BlockID] = true
			dailySessions[cell.SubjectID][cell.Day]++
		}
	}
	errorsFound := 0
	for _, assignment := range class.Assignments {
		if assignment.WeeklyLessons() == 0 {
			continue
		}
		sessions := dailySessions[assignment.SubjectID]
		lessons := dailyLessons[assignment.SubjectID]
		if sessions == nil {
			sessions = make([]int, days)
			lessons = make([]int, days)
		}
		minimum, maximum := sessions[0], sessions[0]
		violatesCap := false
		cap := dailyLessonCap(days, assignment.WeeklyLessons(), assignment.DoubleBlocks)
		for day := 0; day < days; day++ {
			if sessions[day] < minimum {
				minimum = sessions[day]
			}
			if sessions[day] > maximum {
				maximum = sessions[day]
			}
			if lessons[day] > cap {
				violatesCap = true
			}
		}
		if maximum-minimum > 1 || violatesCap {
			errorsFound++
		}
	}
	return errorsFound
}
