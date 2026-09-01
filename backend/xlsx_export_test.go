package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildScheduleWorkbookContainsAllClassSheets(t *testing.T) {
	config := defaultConfig()
	run := ScheduleRun{
		ID: "run-test", Name: "测试课表", Status: "completed", Config: config,
		Schedules:  []ClassSchedule{{ClassID: config.Classes[0].ID}},
		Validation: ValidationReport{Passed: true},
	}
	data, err := buildScheduleWorkbook(run)
	if err != nil {
		t.Fatalf("build workbook: %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open xlsx zip: %v", err)
	}
	entries := make(map[string]string, len(archive.File))
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		content, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatalf("read %s: %v", file.Name, readErr)
		}
		entries[file.Name] = string(content)
	}
	if got, want := len(entries)-5, len(config.Classes)+1; got != want {
		t.Fatalf("worksheet count = %d, want %d", got, want)
	}
	workbook := entries["xl/workbook.xml"]
	if !strings.Contains(workbook, `name="汇总"`) || !strings.Contains(workbook, `name="示例班级 01"`) || !strings.Contains(workbook, `name="示例班级 21"`) {
		t.Fatalf("workbook does not list expected sheets: %s", workbook)
	}
	if !strings.Contains(entries["xl/worksheets/sheet2.xml"], "测试课表") {
		t.Fatal("first class sheet is missing the run title")
	}
}

func TestExportRunEndpointReturnsXLSX(t *testing.T) {
	store := NewMemoryStore()
	config := store.Config()
	store.AddRun(&ScheduleRun{
		ID: "export-test", Name: "导出测试", Status: "completed", Config: config,
		Schedules:  []ClassSchedule{{ClassID: config.Classes[0].ID}},
		Validation: ValidationReport{Passed: true},
	})
	mux := http.NewServeMux()
	NewAPI(store).Register(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/runs/export-test/export.xlsx", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("content type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, "filename*=UTF-8''") || !strings.Contains(got, "%E5%AF%BC%E5%87%BA") {
		t.Fatalf("content disposition = %q", got)
	}
	if !bytes.HasPrefix(recorder.Body.Bytes(), []byte("PK")) {
		t.Fatal("response is not a zip-based xlsx workbook")
	}
}

func TestExportRunEndpointRejectsIncompleteRun(t *testing.T) {
	store := NewMemoryStore()
	store.AddRun(&ScheduleRun{ID: "queued-test", Name: "进行中", Status: "running", Config: store.Config()})
	mux := http.NewServeMux()
	NewAPI(store).Register(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/runs/queued-test/export.xlsx", nil))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
}
