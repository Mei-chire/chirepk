package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseLessonValue(t *testing.T) {
	tests := []struct {
		input       string
		single      int
		double      int
		shouldError bool
	}{
		{input: "6", single: 6},
		{input: "4+3", single: 4, double: 3},
		{input: "4＋2", single: 4, double: 2},
		{input: "", single: 0},
		{input: "4+2+1", shouldError: true},
	}
	for _, test := range tests {
		single, double, err := parseLessonValue(test.input)
		if test.shouldError {
			if err == nil {
				t.Fatalf("parseLessonValue(%q) expected an error", test.input)
			}
			continue
		}
		if err != nil || single != test.single || double != test.double {
			t.Fatalf("parseLessonValue(%q) = %d, %d, %v; want %d, %d", test.input, single, double, err, test.single, test.double)
		}
	}
}

func TestMemoryStoreStartsWithEmptyTeacherData(t *testing.T) {
	store := NewMemoryStore()
	config := store.Config()
	if len(config.Classes) != 21 {
		t.Fatalf("blank config class count = %d, want 21", len(config.Classes))
	}
	if stats := teacherStats(config); len(stats) != 0 {
		t.Fatalf("default teacher stats = %d, want empty", len(stats))
	}
	for _, class := range config.Classes {
		if class.HeadTeacher != "" || len(class.Assignments) != 0 {
			t.Fatalf("class %s still has teacher data: %+v", class.Name, class)
		}
	}
}

func TestImportConfigFromXLSXMapsAliasesAndTeacherlessCourses(t *testing.T) {
	workbook := testImportWorkbook()
	config, err := importConfigFromXLSX(workbook)
	if err != nil {
		t.Fatalf("import workbook: %v", err)
	}
	if got, want := len(config.Classes), 2; got != want {
		t.Fatalf("class count = %d, want %d", got, want)
	}
	class := config.Classes[0]
	if class.ID != "801" || class.Name != "801班" || class.HeadTeacher != "王老师" {
		t.Fatalf("unexpected class metadata: %+v", class)
	}
	assignments := make(map[string]CourseAssignment)
	for _, assignment := range class.Assignments {
		assignments[assignment.SubjectID] = assignment
	}
	if assignments["morality"].Teacher != "政治老师" {
		t.Fatalf("政治 should map to morality: %+v", assignments["morality"])
	}
	if assignments["math"].SingleLessons != 4 || assignments["math"].DoubleBlocks != 3 {
		t.Fatalf("math hours = %+v, want 4+3", assignments["math"])
	}
	for _, subjectID := range []string{"club", "labor", "safety"} {
		if assignments[subjectID].Teacher != "" {
			t.Fatalf("%s must be teacherless, got %q", subjectID, assignments[subjectID].Teacher)
		}
	}
	if assignments["club"].SingleLessons != 2 || assignments["labor"].SingleLessons != 1 {
		t.Fatalf("unexpected teacherless course hours: club=%+v labor=%+v", assignments["club"], assignments["labor"])
	}
	if assignments["chinese"].Teacher != "语文老师" {
		t.Fatalf("unexpected Chinese teacher: %+v", assignments["chinese"])
	}
}

func TestImportEndpointRequiresMultipartFileAndResetsSetup(t *testing.T) {
	store := NewMemoryStore()
	api := NewAPI(store)
	mux := http.NewServeMux()
	api.Register(mux)
	body := &bytes.Buffer{}
	writer := multipartWriter(body, "file", "任课表.xlsx", testImportWorkbook())
	request := httptest.NewRequest(http.MethodPost, "/api/import/xlsx", body)
	request.Header.Set("Content-Type", writer)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if !store.Setup().Imported || store.Setup().AssignmentsReady || store.Setup().TimesReady {
		t.Fatalf("unexpected setup after import: %+v", store.Setup())
	}
	if len(store.Config().Classes) != 2 {
		t.Fatalf("imported class count = %d", len(store.Config().Classes))
	}
	preflight := api.preflightData()
	if preflight["ready"] != false || preflight["message"] != "请先完成并保存每日作息" {
		t.Fatalf("unexpected preflight after import: %#v", preflight)
	}
}

func TestPreflightRequiresBothSavedStagesBeforeScheduling(t *testing.T) {
	config, err := importConfigFromXLSX(testImportWorkbook())
	if err != nil {
		t.Fatalf("import fixture: %v", err)
	}
	store := NewMemoryStore()
	store.ImportConfig(config, "任课表.xlsx")
	api := NewAPI(store)
	mux := http.NewServeMux()
	api.Register(mux)
	postRun := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{"name":"测试"}`)))
		return recorder
	}
	if recorder := postRun(); recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "每日作息") {
		t.Fatalf("run should be blocked before stages: %d %s", recorder.Code, recorder.Body.String())
	}
	configBody, _ := json.Marshal(store.Config())
	timesRequest := httptest.NewRequest(http.MethodPut, "/api/config?stage=times", bytes.NewReader(configBody))
	timesRequest.Header.Set("Content-Type", "application/json")
	timesRecorder := httptest.NewRecorder()
	mux.ServeHTTP(timesRecorder, timesRequest)
	if timesRecorder.Code != http.StatusOK {
		t.Fatalf("save times status = %d: %s", timesRecorder.Code, timesRecorder.Body.String())
	}
	if recorder := postRun(); recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "任课设置") {
		t.Fatalf("run should be blocked before assignments: %d %s", recorder.Code, recorder.Body.String())
	}
	assignmentsRequest := httptest.NewRequest(http.MethodPut, "/api/config?stage=assignments", bytes.NewReader(configBody))
	assignmentsRequest.Header.Set("Content-Type", "application/json")
	assignmentsRecorder := httptest.NewRecorder()
	mux.ServeHTTP(assignmentsRecorder, assignmentsRequest)
	if assignmentsRecorder.Code != http.StatusOK {
		t.Fatalf("save assignments status = %d: %s", assignmentsRecorder.Code, assignmentsRecorder.Body.String())
	}
	if preflight := api.preflightData(); preflight["ready"] != true {
		t.Fatalf("preflight should be ready after both stages: %#v", preflight)
	}
}

func multipartWriter(body *bytes.Buffer, field, filename string, data []byte) string {
	boundary := "chirepk-test-boundary"
	body.WriteString("--" + boundary + "\r\n")
	body.WriteString(`Content-Disposition: form-data; name="` + field + `"; filename="` + filename + `"` + "\r\n")
	body.WriteString("Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet\r\n\r\n")
	body.Write(data)
	body.WriteString("\r\n--" + boundary + "--\r\n")
	return "multipart/form-data; boundary=" + boundary
}

func testImportWorkbook() []byte {
	stringsTable := []string{"年级", "班级", "蹲班干部", "班主任", "语文", "数学", "英语", "物理", "化学", "生物", "政治", "历史", "地理", "音乐", "美术", "体育", "社团", "劳技", "安全", "课时", "八年级", "王老师", "政治老师", "语文老师", "曹正祥"}
	index := make(map[string]int, len(stringsTable))
	for i, value := range stringsTable {
		index[value] = i
	}
	cellShared := func(ref, value string) string {
		return fmt.Sprintf(`<c r="%s" t="s"><v>%d</v></c>`, ref, index[value])
	}
	cellNumber := func(ref, value string) string {
		return fmt.Sprintf(`<c r="%s"><v>%s</v></c>`, ref, value)
	}
	header := []string{"年级", "班级", "蹲班干部", "班主任", "语文", "课时", "数学", "课时", "英语", "课时", "物理", "课时", "化学", "课时", "生物", "课时", "政治", "课时", "历史", "课时", "地理", "课时", "音乐", "课时", "美术", "课时", "体育", "课时", "社团", "课时", "劳技", "课时", "安全", "课时"}
	row := func(rowNumber, class string) string {
		values := []string{"八年级", class, "曹正祥", "王老师", "语文老师", "6", "谭琰", "4+3", "英语老师", "4+2", "物理老师", "5", "", "0", "生物老师", "3", "政治老师", "2", "历史老师", "2", "地理老师", "3", "音乐老师", "1", "美术老师", "1", "体育老师", "1", "", "2", "", "1", "", "0"}
		parts := make([]string, 0, len(values))
		for i, value := range values {
			ref := columnRef(i) + rowNumber
			if i == 1 || i == 5 || i == 7 || i == 9 || i == 11 || i == 13 || i == 15 || i == 17 || i == 19 || i == 21 || i == 23 || i == 25 || i == 27 || i == 29 || i == 31 || i == 33 {
				parts = append(parts, cellNumber(ref, value))
				continue
			}
			if value != "" {
				parts = append(parts, cellShared(ref, value))
			}
		}
		return strings.Join(parts, "")
	}
	headerCells := make([]string, 0, len(header))
	for i, value := range header {
		headerCells = append(headerCells, cellShared(columnRef(i)+"2", value))
	}
	sheet := `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>导入测试</t></is></c></row><row r="2">` + strings.Join(headerCells, "") + `</row><row r="3">` + row("3", "801") + `</row><row r="4">` + row("4", "802") + `</row></sheetData></worksheet>`
	shared := `<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` + func() string {
		items := make([]string, 0, len(stringsTable))
		for _, value := range stringsTable {
			items = append(items, `<si><t>`+value+`</t></si>`)
		}
		return strings.Join(items, "")
	}() + `</sst>`
	files := map[string]string{
		"xl/workbook.xml":            `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="任课课时汇总" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   sheet,
		"xl/sharedStrings.xml":       shared,
		"[Content_Types].xml":        `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"_rels/.rels":                `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`,
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			panic(err)
		}
		_, _ = io.WriteString(entry, content)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return output.Bytes()
}

func columnRef(index int) string {
	index++
	result := ""
	for index > 0 {
		index--
		result = string(rune('A'+index%26)) + result
		index /= 26
	}
	return result
}
