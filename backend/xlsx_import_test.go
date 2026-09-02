package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	if len(config.TimeSlots) != 0 {
		t.Fatalf("blank config time slots = %d, want empty", len(config.TimeSlots))
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
	if class.ID != "示例A" || class.Name != "示例A班" || class.HeadTeacher != "班主任甲" {
		t.Fatalf("unexpected class metadata: %+v", class)
	}
	assignments := make(map[string]CourseAssignment)
	for _, assignment := range class.Assignments {
		assignments[assignment.SubjectID] = assignment
	}
	if assignments["morality"].Teacher != "教师乙" {
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
	if assignments["chinese"].Teacher != "教师丙" {
		t.Fatalf("unexpected Chinese teacher: %+v", assignments["chinese"])
	}
	if got, want := len(config.TimeSlots), 14; got != want {
		t.Fatalf("time slot count = %d, want %d", got, want)
	}
	if config.TimeSlots[0].Start != "08:00" || config.TimeSlots[0].End != "08:40" {
		t.Fatalf("unexpected first imported time slot: %+v", config.TimeSlots[0])
	}
	if got, want := len(schedulableSlots(config)), 9; got != want {
		t.Fatalf("schedulable slots = %d, want %d", got, want)
	}
}

func TestImportEndpointAutoCompletesSetup(t *testing.T) {
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
	if !store.Setup().Imported || !store.Setup().AssignmentsReady || !store.Setup().TimesReady {
		t.Fatalf("unexpected setup after import: %+v", store.Setup())
	}
	if len(store.Config().Classes) != 2 {
		t.Fatalf("imported class count = %d", len(store.Config().Classes))
	}
	preflight := api.preflightData()
	if preflight["ready"] != true || preflight["message"] != "课时容量与必填信息检查通过" {
		t.Fatalf("unexpected preflight after import: %#v", preflight)
	}
	if stats := teacherStats(store.Config()); len(stats) == 0 {
		t.Fatal("teacher statistics should be populated immediately after import")
	}
}

func TestInvalidDailyScheduleDoesNotReplaceImportedConfiguration(t *testing.T) {
	store := NewMemoryStore()
	api := NewAPI(store)
	mux := http.NewServeMux()
	api.Register(mux)
	importFile := func(name string, data []byte) *httptest.ResponseRecorder {
		body := &bytes.Buffer{}
		contentType := multipartWriter(body, "file", name, data)
		request := httptest.NewRequest(http.MethodPost, "/api/import/xlsx", body)
		request.Header.Set("Content-Type", contentType)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		return recorder
	}
	if recorder := importFile("有效.xlsx", testImportWorkbook()); recorder.Code != http.StatusOK {
		t.Fatalf("initial import status = %d: %s", recorder.Code, recorder.Body.String())
	}
	originalConfig := store.Config()
	originalSetup := store.Setup()
	if recorder := importFile("无效.xlsx", testImportWorkbookWithFirstEnd("0.3")); recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid import status = %d, want 422: %s", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(store.Config(), originalConfig) {
		t.Fatal("invalid daily schedule replaced the existing configuration")
	}
	if !reflect.DeepEqual(store.Setup(), originalSetup) {
		t.Fatal("invalid daily schedule changed setup status")
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
	return testImportWorkbookWithFirstEnd("0.3611111111111111")
}

func testImportWorkbookWithFirstEnd(firstEnd string) []byte {
	stringsTable := []string{"年级", "班级", "蹲班干部", "班主任", "语文", "数学", "英语", "物理", "化学", "生物", "政治", "历史", "地理", "音乐", "美术", "体育", "社团", "劳技", "安全", "课时", "示例年级", "班主任甲", "教师乙", "教师丙", "教师甲", "教师丁", "英语教师", "物理教师", "生物教师", "历史教师", "地理教师", "音乐教师", "美术教师", "体育教师"}
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
		values := []string{"示例年级", class, "教师甲", "班主任甲", "教师丙", "6", "教师丁", "4+3", "英语教师", "4+2", "物理教师", "5", "", "0", "生物教师", "3", "教师乙", "2", "历史教师", "2", "地理教师", "3", "音乐教师", "1", "美术教师", "1", "体育教师", "1", "", "2", "", "1", "", "0"}
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
	assignmentSheet := `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>导入测试</t></is></c></row><row r="2">` + strings.Join(headerCells, "") + `</row><row r="3">` + row("3", "示例A") + `</row><row r="4">` + row("4", "示例B") + `</row></sheetData></worksheet>`
	inlineCell := func(ref, value string) string {
		return fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, value)
	}
	type dailyRow struct {
		name, start, end, property string
	}
	dailyRows := []dailyRow{
		{"第一节课", "0.3333333333333333", firstEnd, "可排课"},
		{"大课间", "0.3611111111111111", "0.3819444444444444", "不排课"},
		{"第二节课", "0.3819444444444444", "0.4097222222222222", "可排课"},
		{"第三节课", "0.4201388888888889", "0.4479166666666667", "可排课"},
		{"眼保健操", "0.4479166666666667", "0.4583333333333333", "不排课"},
		{"第四节课", "0.4583333333333333", "0.4861111111111111", "可排课"},
		{"中餐", "0.4861111111111111", "0.5", "不排课"},
		{"午休", "0.5277777777777778", "0.5833333333333334", "不排课"},
		{"第五节课", "0.5972222222222222", "0.625", "可排课"},
		{"第六节课", "0.6319444444444444", "0.6597222222222222", "可排课"},
		{"眼保健操", "0.6597222222222222", "0.6701388888888888", "不排课"},
		{"第七节课", "0.6701388888888888", "0.6979166666666666", "可排课"},
		{"课后服务1", "0.7048611111111112", "0.7291666666666666", "可排课"},
		{"课后服务2", "0.7361111111111112", "0.7569444444444444", "可排课"},
	}
	dailyXMLRows := []string{
		`<row r="1">` + inlineCell("A1", "每日作息导入测试") + `</row>`,
		`<row r="2">` + inlineCell("A2", "序号") + inlineCell("B2", "时段安排") + inlineCell("C2", "开始时间") + inlineCell("D2", "结束时间") + inlineCell("E2", "排课属性") + `</row>`,
	}
	for index, item := range dailyRows {
		rowNumber := index + 3
		dailyXMLRows = append(dailyXMLRows, fmt.Sprintf(`<row r="%d"><c r="A%d"><v>%d</v></c>%s<c r="C%d"><v>%s</v></c><c r="D%d"><v>%s</v></c>%s</row>`,
			rowNumber, rowNumber, index+1, inlineCell(fmt.Sprintf("B%d", rowNumber), item.name), rowNumber, item.start, rowNumber, item.end, inlineCell(fmt.Sprintf("E%d", rowNumber), item.property)))
	}
	dailySheet := `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + strings.Join(dailyXMLRows, "") + `</sheetData></worksheet>`
	shared := `<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` + func() string {
		items := make([]string, 0, len(stringsTable))
		for _, value := range stringsTable {
			items = append(items, `<si><t>`+value+`</t></si>`)
		}
		return strings.Join(items, "")
	}() + `</sst>`
	files := map[string]string{
		"xl/workbook.xml":            `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="每日作息" sheetId="1" r:id="rId1"/><sheet name="任课课时汇总" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="worksheet" Target="worksheets/sheet2.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   dailySheet,
		"xl/worksheets/sheet2.xml":   assignmentSheet,
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
