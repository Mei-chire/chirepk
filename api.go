package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"time"
)

type API struct {
	store *MemoryStore
}

type apiError struct {
	Error string `json:"error"`
}

type runSummary struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	CreatedAt  time.Time        `json:"createdAt"`
	FinishedAt *time.Time       `json:"finishedAt,omitempty"`
	Status     string           `json:"status"`
	Progress   int              `json:"progress"`
	Message    string           `json:"message"`
	Validation ValidationReport `json:"validation"`
}

func NewAPI(store *MemoryStore) *API {
	return &API{store: store}
}

func (api *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", api.health)
	mux.HandleFunc("GET /api/config", api.getConfig)
	mux.HandleFunc("PUT /api/config", api.putConfig)
	mux.HandleFunc("POST /api/config/reset", api.resetConfig)
	mux.HandleFunc("POST /api/import/xlsx", api.importXLSX)
	mux.HandleFunc("GET /api/teachers", api.getTeachers)
	mux.HandleFunc("GET /api/preflight", api.preflight)
	mux.HandleFunc("GET /api/runs", api.listRuns)
	mux.HandleFunc("POST /api/runs", api.createRun)
	mux.HandleFunc("GET /api/runs/{id}", api.getRun)
	mux.HandleFunc("GET /api/runs/{id}/export.xlsx", api.exportRun)
	mux.HandleFunc("DELETE /api/runs/{id}", api.deleteRun)
}

func (api *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "chirepk", "storage": "memory"})
}

func (api *API) getConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.store.Config())
}

func (api *API) putConfig(w http.ResponseWriter, r *http.Request) {
	var config Config
	if err := readJSON(r, &config); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	if err := validateConfigShape(config); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: err.Error()})
		return
	}
	stage := strings.TrimSpace(r.URL.Query().Get("stage"))
	if stage != "" && stage != "assignments" && stage != "times" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "未知的配置保存阶段"})
		return
	}
	api.store.SetConfig(config)
	api.store.MarkStage(stage)
	writeJSON(w, http.StatusOK, api.store.Config())
}

func (api *API) resetConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.store.ResetConfig())
}

func (api *API) importXLSX(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportWorkbookSize+2<<20)
	if err := r.ParseMultipartForm(maxImportWorkbookSize + 2<<20); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "上传文件失败，请选择有效的 .xlsx 文件"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "请选择要导入的 Excel 文件"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxImportWorkbookSize+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "读取上传文件失败"})
		return
	}
	if len(data) > maxImportWorkbookSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, apiError{Error: "上传文件不能超过 20 MB"})
		return
	}
	config, err := importConfigFromXLSX(data)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: err.Error()})
		return
	}
	if err := validateConfigShape(config); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "导入数据校验失败: " + err.Error()})
		return
	}
	sourceName := strings.TrimSpace(strings.ReplaceAll(header.Filename, "\\", "/"))
	sourceName = path.Base(sourceName)
	api.store.ImportConfig(config, sourceName)
	writeJSON(w, http.StatusOK, map[string]any{
		"config":    api.store.Config(),
		"teachers":  teacherStats(api.store.Config()),
		"preflight": api.preflightData(),
	})
}

func (api *API) getTeachers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, teacherStats(api.store.Config()))
}

func (api *API) preflight(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.preflightData())
}

func (api *API) preflightData() map[string]any {
	config := api.store.Config()
	setup := api.store.Setup()
	result := map[string]any{
		"ready":            false,
		"message":          "请先导入任课 Excel",
		"imported":         setup.Imported,
		"assignmentsReady": setup.AssignmentsReady,
		"timesReady":       setup.TimesReady,
		"sourceName":       setup.SourceName,
		"importedAt":       setup.ImportedAt,
		"steps": []map[string]any{
			{"key": "import", "label": "导入任课 Excel", "done": setup.Imported},
			{"key": "check", "label": "检查教师与课时", "done": setup.Imported},
			{"key": "times", "label": "完成每日作息", "done": setup.TimesReady},
			{"key": "assignments", "label": "保存任课设置", "done": setup.AssignmentsReady},
			{"key": "ready", "label": "开始排课", "done": false},
		},
	}
	if !setup.Imported {
		return result
	}
	if !setup.TimesReady {
		result["message"] = "请先完成并保存每日作息"
		return result
	}
	if !setup.AssignmentsReady {
		result["message"] = "请先完成并保存任课设置"
		return result
	}
	if err := validateForScheduling(config, schedulableSlots(config)); err != nil {
		result["message"] = err.Error()
		return result
	}
	total := 0
	for _, class := range config.Classes {
		for _, assignment := range class.Assignments {
			total += assignment.WeeklyLessons()
		}
	}
	result["ready"] = true
	result["message"] = "课时容量与必填信息检查通过"
	result["classes"] = len(config.Classes)
	result["lessons"] = total
	steps := result["steps"].([]map[string]any)
	steps[len(steps)-1]["done"] = true
	return result
}

func (api *API) listRuns(w http.ResponseWriter, _ *http.Request) {
	runs := api.store.Runs()
	summaries := make([]runSummary, 0, len(runs))
	for _, run := range runs {
		summaries = append(summaries, summarizeRun(run))
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (api *API) createRun(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &request); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	config := api.store.Config()
	preflight := api.preflightData()
	if ready, _ := preflight["ready"].(bool); !ready {
		message, _ := preflight["message"].(string)
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: message})
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = "自动排课 " + time.Now().Format("01-02 15:04")
	}
	run := &ScheduleRun{
		ID: newID(), Name: name, CreatedAt: time.Now(), Status: "queued", Progress: 0,
		Message: "任务已创建", Config: config,
	}
	api.store.AddRun(run)
	go api.generate(run.ID, config)
	writeJSON(w, http.StatusAccepted, summarizeRun(*run))
}

func (api *API) generate(id string, config Config) {
	api.store.UpdateRun(id, func(run *ScheduleRun) {
		run.Status = "running"
		run.Progress = 1
		run.Message = "正在启动排课器"
	})
	progress := func(percent int, message string) {
		api.store.UpdateRun(id, func(run *ScheduleRun) {
			if percent > run.Progress {
				run.Progress = percent
			}
			run.Message = message
		})
	}
	schedules, report, err := GenerateSchedule(context.Background(), config, progress)
	finished := time.Now()
	api.store.UpdateRun(id, func(run *ScheduleRun) {
		run.FinishedAt = &finished
		run.Validation = report
		if err != nil {
			run.Status = "failed"
			run.Message = err.Error()
			log.Printf("schedule run %s failed: %v", id, err)
			return
		}
		run.Status = "completed"
		run.Progress = 100
		run.Message = "排课完成，全部约束已通过"
		run.Schedules = schedules
	})
}

func (api *API) getRun(w http.ResponseWriter, r *http.Request) {
	run, ok := api.store.Run(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, apiError{Error: "排课记录不存在"})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (api *API) exportRun(w http.ResponseWriter, r *http.Request) {
	run, ok := api.store.Run(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, apiError{Error: "排课记录不存在"})
		return
	}
	if run.Status != "completed" || len(run.Schedules) == 0 {
		writeJSON(w, http.StatusConflict, apiError{Error: "排课尚未完成，暂不能导出课表"})
		return
	}
	workbook, err := buildScheduleWorkbook(run)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", contentDispositionFilename(exportFileName(run.Name)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(workbook)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(workbook)
}

func (api *API) deleteRun(w http.ResponseWriter, r *http.Request) {
	run, ok := api.store.Run(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, apiError{Error: "排课记录不存在"})
		return
	}
	if run.Status == "queued" || run.Status == "running" {
		writeJSON(w, http.StatusConflict, apiError{Error: "排课进行中，暂不能删除"})
		return
	}
	api.store.DeleteRun(run.ID)
	w.WriteHeader(http.StatusNoContent)
}

func summarizeRun(run ScheduleRun) runSummary {
	return runSummary{
		ID: run.ID, Name: run.Name, CreatedAt: run.CreatedAt, FinishedAt: run.FinishedAt,
		Status: run.Status, Progress: run.Progress, Message: run.Message, Validation: run.Validation,
	}
}

func readJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求数据格式错误: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newID() string {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}
