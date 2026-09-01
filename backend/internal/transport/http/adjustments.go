package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"chirepk/backend/internal/application"
	"chirepk/backend/internal/domain"
	"chirepk/backend/internal/store"
)

type adjustmentRequest struct {
	ClassID          string                  `json:"classId"`
	Source           domain.SchedulePosition `json:"source"`
	Target           domain.SchedulePosition `json:"target"`
	ExpectedRevision int                     `json:"expectedRevision"`
}

type undoAdjustmentRequest struct {
	ExpectedRevision int `json:"expectedRevision"`
}

func (api *API) getAdjustmentCandidates(w http.ResponseWriter, r *http.Request) {
	day, dayErr := strconv.Atoi(r.URL.Query().Get("day"))
	period, periodErr := strconv.Atoi(r.URL.Query().Get("period"))
	classID := strings.TrimSpace(r.URL.Query().Get("classId"))
	if dayErr != nil || periodErr != nil || classID == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "请选择有效的课程位置"})
		return
	}
	response, found, err := api.service.AdjustmentCandidates(r.PathValue("id"), classID, domain.SchedulePosition{Day: day, Period: period})
	if !found {
		writeJSON(w, http.StatusNotFound, apiError{Error: "排课记录不存在"})
		return
	}
	if err != nil {
		status := http.StatusUnprocessableEntity
		if strings.Contains(err.Error(), "只有已完成") {
			status = http.StatusConflict
		}
		writeJSON(w, status, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (api *API) applyAdjustment(w http.ResponseWriter, r *http.Request) {
	var request adjustmentRequest
	if err := readJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	run, found, err := api.service.ApplyAdjustment(r.PathValue("id"), application.AdjustmentRequest{
		ClassID: request.ClassID, Source: request.Source, Target: request.Target, ExpectedRevision: request.ExpectedRevision,
	})
	if !found {
		writeJSON(w, http.StatusNotFound, apiError{Error: "排课记录不存在"})
		return
	}
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, store.ErrRunRevisionConflict) {
			status = http.StatusConflict
		}
		writeJSON(w, status, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (api *API) undoAdjustment(w http.ResponseWriter, r *http.Request) {
	var request undoAdjustmentRequest
	if err := readJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: err.Error()})
		return
	}
	run, found, err := api.service.UndoAdjustment(r.PathValue("id"), application.UndoAdjustmentRequest{
		ExpectedRevision: request.ExpectedRevision,
	})
	if !found {
		writeJSON(w, http.StatusNotFound, apiError{Error: "排课记录不存在"})
		return
	}
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, store.ErrRunRevisionConflict) {
			status = http.StatusConflict
		}
		writeJSON(w, status, apiError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, run)
}
