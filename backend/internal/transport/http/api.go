package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"chirepk/backend/internal/application"
	"chirepk/backend/internal/domain"
)

type API struct {
	service *application.Service
}

type apiError struct {
	Error string `json:"error"`
}

type runSummary struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	CreatedAt  time.Time               `json:"createdAt"`
	FinishedAt *time.Time              `json:"finishedAt,omitempty"`
	Status     string                  `json:"status"`
	Progress   int                     `json:"progress"`
	Message    string                  `json:"message"`
	Validation domain.ValidationReport `json:"validation"`
}

func NewAPI(service *application.Service) *API {
	return &API{service: service}
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
	mux.HandleFunc("GET /api/runs/{id}/adjustment-candidates", api.getAdjustmentCandidates)
	mux.HandleFunc("POST /api/runs/{id}/adjustments", api.applyAdjustment)
	mux.HandleFunc("POST /api/runs/{id}/adjustments/undo", api.undoAdjustment)
	mux.HandleFunc("GET /api/runs/{id}/export.xlsx", api.exportRun)
	mux.HandleFunc("DELETE /api/runs/{id}", api.deleteRun)
}

func (api *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "chirepk", "storage": "memory"})
}

func (api *API) getConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.service.Config())
}

func (api *API) putConfig(w http.ResponseWriter, r *http.Request) {
	var config domain.Config
	if err := readJSON(r, &config); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	stage := strings.TrimSpace(r.URL.Query().Get("stage"))
	if stage != "" && stage != "assignments" && stage != "times" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "未知的配置保存阶段"})
		return
	}
	updated, err := api.service.SaveConfig(config, stage)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (api *API) resetConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.service.ResetConfig())
}

func (api *API) importXLSX(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, application.MaxImportWorkbookSize+2<<20)
	if err := r.ParseMultipartForm(application.MaxImportWorkbookSize + 2<<20); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "上传文件失败，请选择有效的 .xlsx 文件"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "请选择要导入的 Excel 文件"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, application.MaxImportWorkbookSize+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "读取上传文件失败"})
		return
	}
	if len(data) > application.MaxImportWorkbookSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, apiError{Error: "上传文件不能超过 20 MB"})
		return
	}
	sourceName := path.Base(strings.TrimSpace(strings.ReplaceAll(header.Filename, "\\", "/")))
	result, err := api.service.ImportWorkbook(data, sourceName)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) getTeachers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.service.Teachers())
}

func (api *API) preflight(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.service.Preflight())
}

func (api *API) listRuns(w http.ResponseWriter, _ *http.Request) {
	runs := api.service.ListRuns()
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
	run, err := api.service.CreateRun(request.Name)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, summarizeRun(run))
}

func (api *API) getRun(w http.ResponseWriter, r *http.Request) {
	run, ok := api.service.GetRun(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, apiError{Error: "排课记录不存在"})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (api *API) exportRun(w http.ResponseWriter, r *http.Request) {
	if _, ok := api.service.GetRun(r.PathValue("id")); !ok {
		writeJSON(w, http.StatusNotFound, apiError{Error: "排课记录不存在"})
		return
	}
	workbook, filename, err := api.service.ExportRun(r.PathValue("id"))
	if err != nil {
		status := http.StatusConflict
		if !strings.Contains(err.Error(), "尚未完成") {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, apiError{Error: err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", contentDispositionFilename(filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(workbook)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(workbook)
}

func (api *API) deleteRun(w http.ResponseWriter, r *http.Request) {
	found, err := api.service.DeleteRun(r.PathValue("id"))
	if !found {
		writeJSON(w, http.StatusNotFound, apiError{Error: "排课记录不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, apiError{Error: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func summarizeRun(run domain.ScheduleRun) runSummary {
	return runSummary{ID: run.ID, Name: run.Name, CreatedAt: run.CreatedAt, FinishedAt: run.FinishedAt, Status: run.Status, Progress: run.Progress, Message: run.Message, Validation: run.Validation}
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
