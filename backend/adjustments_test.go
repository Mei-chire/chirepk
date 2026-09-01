package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func generatedRunForAdjustmentTest(t *testing.T) ScheduleRun {
	t.Helper()
	config := defaultConfig()
	schedules, report, err := GenerateSchedule(context.Background(), config, nil)
	if err != nil {
		t.Fatalf("generate schedule: %v", err)
	}
	if !report.Passed {
		t.Fatalf("generated schedule did not validate: %+v", report)
	}
	return ScheduleRun{ID: "test-run", Status: "completed", Config: config, Schedules: schedules, Validation: report}
}

func TestAdjustmentCandidatesAlwaysPassFullValidation(t *testing.T) {
	run := generatedRunForAdjustmentTest(t)
	var response adjustmentCandidatesResponse
	found := false
	for _, cell := range run.Schedules[0].Cells {
		candidateResponse, err := buildAdjustmentCandidates(run, run.Schedules[0].ClassID, SchedulePosition{Day: cell.Day, Period: cell.Period})
		if err != nil {
			t.Fatalf("build candidates: %v", err)
		}
		if len(candidateResponse.Candidates) > 0 {
			response = candidateResponse
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected at least one safe adjustment candidate")
	}

	for _, candidate := range response.Candidates {
		trial := clone(run.Schedules)
		if err := swapScheduleRanges(run.Config, trial, run.Schedules[0].ClassID, response.SourcePositions, candidate.TargetPositions); err != nil {
			t.Fatalf("swap candidate: %v", err)
		}
		if report := ValidateSchedule(run.Config, trial); !report.Passed {
			t.Fatalf("candidate failed validation: %+v", report)
		}
	}
}

func TestAdjustmentRejectsPartialDoubleBlock(t *testing.T) {
	run := generatedRunForAdjustmentTest(t)
	schedule := run.Schedules[0]
	var single, double ScheduleCell
	for _, cell := range schedule.Cells {
		if cell.BlockID == "" && single.SubjectID == "" {
			single = cell
		}
		if cell.BlockID != "" && double.SubjectID == "" {
			double = cell
		}
	}
	if single.SubjectID == "" || double.SubjectID == "" {
		t.Fatal("expected both single and double lessons")
	}

	_, _, err := prepareScheduleSwap(
		run.Config,
		run.Schedules,
		schedule.ClassID,
		SchedulePosition{Day: single.Day, Period: single.Period},
		SchedulePosition{Day: double.Day, Period: double.Period},
	)
	if err == nil {
		t.Fatal("expected a partial double-block swap to be rejected")
	}
}

func TestScheduleSwapIsReversible(t *testing.T) {
	run := generatedRunForAdjustmentTest(t)
	response, err := buildAdjustmentCandidates(run, run.Schedules[0].ClassID, SchedulePosition{
		Day: run.Schedules[0].Cells[0].Day, Period: run.Schedules[0].Cells[0].Period,
	})
	if err != nil {
		t.Fatalf("build candidates: %v", err)
	}
	if len(response.Candidates) == 0 {
		t.Skip("selected generated lesson has no reversible candidate")
	}
	original := clone(run.Schedules)
	target := response.Candidates[0].TargetPositions
	if err := swapScheduleRanges(run.Config, run.Schedules, run.Schedules[0].ClassID, response.SourcePositions, target); err != nil {
		t.Fatalf("apply swap: %v", err)
	}
	if err := swapScheduleRanges(run.Config, run.Schedules, run.Schedules[0].ClassID, response.SourcePositions, target); err != nil {
		t.Fatalf("undo swap: %v", err)
	}
	if !reflect.DeepEqual(run.Schedules, original) {
		t.Fatal("swapping the same ranges twice should restore the schedule")
	}
}

func TestValidationDetectsDuplicateClassPosition(t *testing.T) {
	run := generatedRunForAdjustmentTest(t)
	run.Schedules[0].Cells[1].Day = run.Schedules[0].Cells[0].Day
	run.Schedules[0].Cells[1].Period = run.Schedules[0].Cells[0].Period
	run.Schedules[0].Cells[1].SlotID = run.Schedules[0].Cells[0].SlotID
	report := ValidateSchedule(run.Config, run.Schedules)
	if report.Passed || report.CountMismatches == 0 {
		t.Fatalf("expected duplicate position to fail validation: %+v", report)
	}
}

func TestMutateRunRejectsStaleRevision(t *testing.T) {
	store := NewMemoryStore()
	store.AddRun(&ScheduleRun{ID: "run-1", Revision: 2})
	_, found, err := store.MutateRun("run-1", 1, func(run *ScheduleRun) error {
		run.Revision++
		return nil
	})
	if !found || !errors.Is(err, ErrRunRevisionConflict) {
		t.Fatalf("expected revision conflict, found=%v err=%v", found, err)
	}
	run, _ := store.Run("run-1")
	if run.Revision != 2 {
		t.Fatalf("stale mutation changed revision to %d", run.Revision)
	}
}

func TestAdjustmentAPIApplyAndUndo(t *testing.T) {
	run := generatedRunForAdjustmentTest(t)
	original := clone(run.Schedules)
	var source SchedulePosition
	var candidate adjustmentCandidate
	found := false
	for _, cell := range run.Schedules[0].Cells {
		response, err := buildAdjustmentCandidates(run, run.Schedules[0].ClassID, SchedulePosition{Day: cell.Day, Period: cell.Period})
		if err != nil {
			t.Fatalf("build candidates: %v", err)
		}
		if len(response.Candidates) > 0 {
			source = response.SourcePositions[0]
			candidate = response.Candidates[0]
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a safe API adjustment candidate")
	}

	store := NewMemoryStore()
	store.AddRun(&run)
	api := NewAPI(store)
	mux := http.NewServeMux()
	api.Register(mux)
	requestBody, _ := json.Marshal(adjustmentRequest{
		ClassID: run.Schedules[0].ClassID, Source: source,
		Target: candidate.TargetPositions[0], ExpectedRevision: 0,
	})
	applyRequest := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/adjustments", bytes.NewReader(requestBody))
	applyRequest.Header.Set("Content-Type", "application/json")
	applyRecorder := httptest.NewRecorder()
	mux.ServeHTTP(applyRecorder, applyRequest)
	if applyRecorder.Code != http.StatusOK {
		t.Fatalf("apply status = %d: %s", applyRecorder.Code, applyRecorder.Body.String())
	}
	var adjusted ScheduleRun
	if err := json.NewDecoder(applyRecorder.Body).Decode(&adjusted); err != nil {
		t.Fatal(err)
	}
	if adjusted.Revision != 1 || len(adjusted.Adjustments) != 1 || !adjusted.Validation.Passed {
		t.Fatalf("unexpected adjusted run: revision=%d adjustments=%d validation=%+v", adjusted.Revision, len(adjusted.Adjustments), adjusted.Validation)
	}

	staleRequest := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/adjustments", bytes.NewReader(requestBody))
	staleRequest.Header.Set("Content-Type", "application/json")
	staleRecorder := httptest.NewRecorder()
	mux.ServeHTTP(staleRecorder, staleRequest)
	if staleRecorder.Code != http.StatusConflict {
		t.Fatalf("stale apply status = %d, want 409: %s", staleRecorder.Code, staleRecorder.Body.String())
	}

	undoBody, _ := json.Marshal(undoAdjustmentRequest{ExpectedRevision: 1})
	undoRequest := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/adjustments/undo", bytes.NewReader(undoBody))
	undoRequest.Header.Set("Content-Type", "application/json")
	undoRecorder := httptest.NewRecorder()
	mux.ServeHTTP(undoRecorder, undoRequest)
	if undoRecorder.Code != http.StatusOK {
		t.Fatalf("undo status = %d: %s", undoRecorder.Code, undoRecorder.Body.String())
	}
	var restored ScheduleRun
	if err := json.NewDecoder(undoRecorder.Body).Decode(&restored); err != nil {
		t.Fatal(err)
	}
	if restored.Revision != 2 || len(restored.Adjustments) != 0 || !reflect.DeepEqual(restored.Schedules, original) {
		t.Fatalf("undo did not restore original schedule: revision=%d adjustments=%d", restored.Revision, len(restored.Adjustments))
	}
}
