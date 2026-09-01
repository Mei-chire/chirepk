package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"chirepk/backend/internal/adapter/xlsx"
	"chirepk/backend/internal/domain"
	"chirepk/backend/internal/scheduler"
	"chirepk/backend/internal/store"
)

const MaxImportWorkbookSize = xlsx.MaxImportWorkbookSize

type Service struct {
	repo  store.Repository
	now   func() time.Time
	newID func() string
	log   *log.Logger
}

func NewService(repo store.Repository) *Service {
	return &Service{repo: repo, now: time.Now, newID: newID, log: log.Default()}
}

type PreflightStep struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Done  bool   `json:"done"`
}

type PreflightResult struct {
	Ready            bool            `json:"ready"`
	Message          string          `json:"message"`
	Imported         bool            `json:"imported"`
	AssignmentsReady bool            `json:"assignmentsReady"`
	TimesReady       bool            `json:"timesReady"`
	SourceName       string          `json:"sourceName,omitempty"`
	ImportedAt       *time.Time      `json:"importedAt,omitempty"`
	Classes          int             `json:"classes,omitempty"`
	Lessons          int             `json:"lessons,omitempty"`
	Steps            []PreflightStep `json:"steps"`
}

type ImportResult struct {
	Config    domain.Config        `json:"config"`
	Teachers  []domain.TeacherStat `json:"teachers"`
	Preflight PreflightResult      `json:"preflight"`
}

type AdjustmentRequest struct {
	ClassID          string
	Source           domain.SchedulePosition
	Target           domain.SchedulePosition
	ExpectedRevision int
}

type UndoAdjustmentRequest struct {
	ExpectedRevision int
}

func (s *Service) Config() domain.Config {
	return s.repo.Config()
}

func (s *Service) SaveConfig(config domain.Config, stage string) (domain.Config, error) {
	if stage != "" && stage != "assignments" && stage != "times" {
		return domain.Config{}, errors.New("未知的配置保存阶段")
	}
	if err := domain.ValidateConfigShape(config); err != nil {
		return domain.Config{}, err
	}
	s.repo.SetConfig(config)
	s.repo.MarkStage(stage)
	return s.repo.Config(), nil
}

func (s *Service) ResetConfig() domain.Config {
	return s.repo.ResetConfig()
}

func (s *Service) ImportWorkbook(data []byte, sourceName string) (ImportResult, error) {
	config, err := xlsx.ImportConfigFromXLSX(data)
	if err != nil {
		return ImportResult{}, err
	}
	if err := domain.ValidateConfigShape(config); err != nil {
		return ImportResult{}, fmt.Errorf("导入数据校验失败: %w", err)
	}
	if err := scheduler.ValidateForScheduling(config, scheduler.SchedulableSlots(config)); err != nil {
		return ImportResult{}, fmt.Errorf("导入数据尚不能直接排课: %w", err)
	}
	s.repo.ImportConfig(config, strings.TrimSpace(sourceName))
	current := s.repo.Config()
	return ImportResult{Config: current, Teachers: domain.TeacherStats(current), Preflight: s.Preflight()}, nil
}

func (s *Service) Teachers() []domain.TeacherStat {
	return domain.TeacherStats(s.repo.Config())
}

func (s *Service) Preflight() PreflightResult {
	setup := s.repo.Setup()
	result := PreflightResult{
		Message: "请先导入任课 Excel", Imported: setup.Imported,
		AssignmentsReady: setup.AssignmentsReady, TimesReady: setup.TimesReady,
		SourceName: setup.SourceName, ImportedAt: setup.ImportedAt,
		Steps: []PreflightStep{
			{Key: "import", Label: "导入任课 Excel", Done: setup.Imported},
			{Key: "check", Label: "检查教师与课时", Done: setup.Imported},
			{Key: "times", Label: "完成每日作息", Done: setup.TimesReady},
			{Key: "assignments", Label: "保存任课设置", Done: setup.AssignmentsReady},
			{Key: "ready", Label: "开始排课"},
		},
	}
	if !setup.Imported {
		return result
	}
	if !setup.TimesReady {
		result.Message = "请先完成并保存每日作息"
		return result
	}
	if !setup.AssignmentsReady {
		result.Message = "请先完成并保存任课设置"
		return result
	}
	config := s.repo.Config()
	if err := scheduler.ValidateForScheduling(config, scheduler.SchedulableSlots(config)); err != nil {
		result.Message = err.Error()
		return result
	}
	for _, class := range config.Classes {
		for _, assignment := range class.Assignments {
			result.Lessons += assignment.WeeklyLessons()
		}
	}
	result.Ready = true
	result.Classes = len(config.Classes)
	result.Message = "课时容量与必填信息检查通过"
	result.Steps[len(result.Steps)-1].Done = true
	return result
}

func (s *Service) ListRuns() []domain.ScheduleRun {
	return s.repo.Runs()
}

func (s *Service) CreateRun(name string) (domain.ScheduleRun, error) {
	preflight := s.Preflight()
	if !preflight.Ready {
		return domain.ScheduleRun{}, errors.New(preflight.Message)
	}
	name = strings.TrimSpace(name)
	now := s.now()
	if name == "" {
		name = "自动排课 " + now.Format("01-02 15:04")
	}
	run := &domain.ScheduleRun{ID: s.newID(), Name: name, CreatedAt: now, Status: "queued", Message: "任务已创建", Config: s.repo.Config()}
	s.repo.AddRun(run)
	go s.generate(run.ID, run.Config)
	return *run, nil
}

func (s *Service) GetRun(id string) (domain.ScheduleRun, bool) {
	return s.repo.Run(id)
}

func (s *Service) DeleteRun(id string) (bool, error) {
	run, found := s.repo.Run(id)
	if !found {
		return false, nil
	}
	if run.Status == "queued" || run.Status == "running" {
		return true, errors.New("排课进行中，暂不能删除")
	}
	s.repo.DeleteRun(id)
	return true, nil
}

func (s *Service) ExportRun(id string) ([]byte, string, error) {
	run, found := s.repo.Run(id)
	if !found {
		return nil, "", nil
	}
	if run.Status != "completed" || len(run.Schedules) == 0 {
		return nil, "", errors.New("排课尚未完成，暂不能导出课表")
	}
	workbook, err := xlsx.BuildScheduleWorkbook(run)
	if err != nil {
		return nil, "", err
	}
	return workbook, xlsx.ExportFileName(run.Name), nil
}

func (s *Service) AdjustmentCandidates(id, classID string, source domain.SchedulePosition) (scheduler.AdjustmentCandidatesResponse, bool, error) {
	run, found := s.repo.Run(id)
	if !found {
		return scheduler.AdjustmentCandidatesResponse{}, false, nil
	}
	if run.Status != "completed" {
		return scheduler.AdjustmentCandidatesResponse{}, true, errors.New("只有已完成的课表可以调整")
	}
	response, err := scheduler.BuildAdjustmentCandidates(run, classID, source)
	return response, true, err
}

func (s *Service) ApplyAdjustment(id string, request AdjustmentRequest) (domain.ScheduleRun, bool, error) {
	run, found, err := s.repo.MutateRun(id, request.ExpectedRevision, func(run *domain.ScheduleRun) error {
		if run.Status != "completed" {
			return errors.New("只有已完成的课表可以调整")
		}
		source, target, err := scheduler.PrepareScheduleSwap(run.Config, run.Schedules, request.ClassID, request.Source, request.Target)
		if err != nil {
			return err
		}
		if err := scheduler.SwapScheduleRanges(run.Config, run.Schedules, request.ClassID, source, target); err != nil {
			return err
		}
		report := scheduler.ValidateSchedule(run.Config, run.Schedules)
		if !report.Passed {
			return errors.New("该交换不再满足全部硬约束，请重新选择")
		}
		run.Revision++
		run.Validation = report
		run.Adjustments = append(run.Adjustments, domain.ScheduleAdjustment{ID: s.newID(), AppliedAt: s.now(), ClassID: request.ClassID, Source: source, Target: target})
		run.Message = fmt.Sprintf("已手动调整 %d 次，全部约束已通过", len(run.Adjustments))
		return nil
	})
	return run, found, err
}

func (s *Service) UndoAdjustment(id string, request UndoAdjustmentRequest) (domain.ScheduleRun, bool, error) {
	run, found, err := s.repo.MutateRun(id, request.ExpectedRevision, func(run *domain.ScheduleRun) error {
		if len(run.Adjustments) == 0 {
			return errors.New("当前没有可撤销的调整")
		}
		last := run.Adjustments[len(run.Adjustments)-1]
		if err := scheduler.SwapScheduleRanges(run.Config, run.Schedules, last.ClassID, last.Source, last.Target); err != nil {
			return err
		}
		report := scheduler.ValidateSchedule(run.Config, run.Schedules)
		if !report.Passed {
			return errors.New("撤销后课表未通过完整校验")
		}
		run.Adjustments = run.Adjustments[:len(run.Adjustments)-1]
		run.Revision++
		run.Validation = report
		if len(run.Adjustments) == 0 {
			run.Message = "排课完成，全部约束已通过"
		} else {
			run.Message = fmt.Sprintf("已手动调整 %d 次，全部约束已通过", len(run.Adjustments))
		}
		return nil
	})
	return run, found, err
}

func (s *Service) generate(id string, config domain.Config) {
	s.repo.UpdateRun(id, func(run *domain.ScheduleRun) {
		run.Status = "running"
		run.Progress = 1
		run.Message = "正在启动排课器"
	})
	progress := func(percent int, message string) {
		s.repo.UpdateRun(id, func(run *domain.ScheduleRun) {
			if percent > run.Progress {
				run.Progress = percent
			}
			run.Message = message
		})
	}
	schedules, report, err := scheduler.GenerateSchedule(context.Background(), config, progress)
	finished := s.now()
	s.repo.UpdateRun(id, func(run *domain.ScheduleRun) {
		run.FinishedAt = &finished
		run.Validation = report
		if err != nil {
			run.Status = "failed"
			run.Message = err.Error()
			s.log.Printf("schedule run %s failed: %v", id, err)
			return
		}
		run.Status = "completed"
		run.Progress = 100
		run.Message = "排课完成，全部约束已通过"
		run.Schedules = schedules
	})
}

func newID() string {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}
