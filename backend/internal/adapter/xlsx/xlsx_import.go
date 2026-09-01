package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
)

const maxImportWorkbookSize = 20 << 20

type xlsxWorkbook struct {
	Sheets struct {
		Items []xlsxSheet `xml:"sheet"`
	} `xml:"sheets"`
}

type xlsxSheet struct {
	Name  string `xml:"name,attr"`
	State string `xml:"state,attr"`
	RelID string `xml:"id,attr"`
}

type xlsxRelationships struct {
	Items []xlsxRelationship `xml:"Relationship"`
}

type xlsxRelationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
}

type xlsxSharedStrings struct {
	Items []xlsxSharedString `xml:"si"`
}

type xlsxSharedString struct {
	Text []string `xml:"t"`
}

type xlsxWorksheet struct {
	SheetData xlsxSheetData `xml:"sheetData"`
}

type xlsxSheetData struct {
	Rows []xlsxRow `xml:"row"`
}

type xlsxRow struct {
	Number int        `xml:"r,attr"`
	Cells  []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Ref    string        `xml:"r,attr"`
	Type   string        `xml:"t,attr"`
	Value  string        `xml:"v"`
	Inline xlsxInlineStr `xml:"is"`
}

type xlsxInlineStr struct {
	Text []string `xml:"t"`
}

type importedWorksheet struct {
	Name string
	Data xlsxWorksheet
}

// importConfigFromXLSX reads both authoritative setup sheets. The caller only
// commits the returned configuration after all workbook data has been parsed.
func importConfigFromXLSX(data []byte) (Config, error) {
	if len(data) == 0 {
		return Config{}, errors.New("上传文件为空")
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Config{}, fmt.Errorf("无法读取 XLSX 文件: %w", err)
	}
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		files[file.Name] = file
	}
	workbookBytes, err := readZipEntry(files, "xl/workbook.xml")
	if err != nil {
		return Config{}, errors.New("XLSX 缺少工作簿信息")
	}
	var workbook xlsxWorkbook
	if err := xml.Unmarshal(workbookBytes, &workbook); err != nil {
		return Config{}, fmt.Errorf("工作簿格式错误: %w", err)
	}
	relsBytes, err := readZipEntry(files, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return Config{}, errors.New("XLSX 缺少工作表关系信息")
	}
	var rels xlsxRelationships
	if err := xml.Unmarshal(relsBytes, &rels); err != nil {
		return Config{}, fmt.Errorf("工作表关系格式错误: %w", err)
	}
	sharedStrings, err := readSharedStrings(files)
	if err != nil {
		return Config{}, err
	}
	relTargets := make(map[string]string, len(rels.Items))
	for _, relationship := range rels.Items {
		relTargets[relationship.ID] = relationship.Target
	}
	worksheets := make([]importedWorksheet, 0, len(workbook.Sheets.Items))
	for _, sheet := range workbook.Sheets.Items {
		if sheet.State == "hidden" || sheet.State == "veryHidden" {
			continue
		}
		target := strings.TrimPrefix(relTargets[sheet.RelID], "/")
		if target == "" {
			return Config{}, fmt.Errorf("找不到工作表“%s”的关系信息", sheet.Name)
		}
		if !strings.HasPrefix(target, "xl/") {
			target = path.Join("xl", target)
		}
		worksheetBytes, err := readZipEntry(files, path.Clean(target))
		if err != nil {
			return Config{}, fmt.Errorf("找不到工作表“%s”的数据", sheet.Name)
		}
		var worksheet xlsxWorksheet
		if err := xml.Unmarshal(worksheetBytes, &worksheet); err != nil {
			return Config{}, fmt.Errorf("工作表“%s”格式错误: %w", sheet.Name, err)
		}
		worksheets = append(worksheets, importedWorksheet{Name: sheet.Name, Data: worksheet})
	}
	if len(worksheets) == 0 {
		return Config{}, errors.New("XLSX 中没有可读取的工作表")
	}
	dailySheet, ok := findImportedWorksheet(worksheets, "每日作息")
	if !ok {
		return Config{}, errors.New("未找到“每日作息”工作表")
	}
	assignmentsSheet, ok := findImportedWorksheet(worksheets, "任课", "课时")
	if !ok {
		return Config{}, errors.New("未找到“任课课时汇总”工作表")
	}
	config, err := configFromAssignmentWorksheet(assignmentsSheet.Data, sharedStrings)
	if err != nil {
		return Config{}, fmt.Errorf("任课课时汇总导入失败: %w", err)
	}
	config.TimeSlots, err = timeSlotsFromWorksheet(dailySheet.Data, sharedStrings)
	if err != nil {
		return Config{}, fmt.Errorf("每日作息导入失败: %w", err)
	}
	return config, nil
}

func findImportedWorksheet(worksheets []importedWorksheet, keywords ...string) (importedWorksheet, bool) {
	for _, worksheet := range worksheets {
		matched := true
		for _, keyword := range keywords {
			if !strings.Contains(strings.TrimSpace(worksheet.Name), keyword) {
				matched = false
				break
			}
		}
		if matched {
			return worksheet, true
		}
	}
	return importedWorksheet{}, false
}

func readZipEntry(files map[string]*zip.File, name string) ([]byte, error) {
	file, ok := files[name]
	if !ok {
		return nil, osErrNotExist{name}
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, maxImportWorkbookSize))
}

type osErrNotExist struct{ name string }

func (e osErrNotExist) Error() string { return "missing zip entry: " + e.name }

func readSharedStrings(files map[string]*zip.File) ([]string, error) {
	data, err := readZipEntry(files, "xl/sharedStrings.xml")
	if err != nil {
		// sharedStrings.xml is optional; inline strings are valid XLSX cells.
		return nil, nil
	}
	var document xlsxSharedStrings
	if err := xml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("共享字符串格式错误: %w", err)
	}
	result := make([]string, len(document.Items))
	for index, item := range document.Items {
		result[index] = strings.Join(item.Text, "")
	}
	return result, nil
}

func worksheetRows(worksheet xlsxWorksheet, sharedStrings []string) (map[int]map[int]string, int, error) {
	rows := make(map[int]map[int]string)
	maxRow := 0
	for rowIndex, row := range worksheet.SheetData.Rows {
		number := row.Number
		if number == 0 {
			number = rowIndex + 1
		}
		if number > maxRow {
			maxRow = number
		}
		cells := make(map[int]string)
		for _, cell := range row.Cells {
			column, ok := xlsxColumnIndex(cell.Ref)
			if !ok {
				continue
			}
			value, err := xlsxCellValue(cell, sharedStrings)
			if err != nil {
				return nil, 0, err
			}
			cells[column] = value
		}
		rows[number] = cells
	}
	return rows, maxRow, nil
}

func configFromAssignmentWorksheet(worksheet xlsxWorksheet, sharedStrings []string) (Config, error) {
	rows, maxRow, err := worksheetRows(worksheet, sharedStrings)
	if err != nil {
		return Config{}, err
	}
	headerRow := 0
	for rowNumber := 1; rowNumber <= maxRow && rowNumber <= 20; rowNumber++ {
		for _, value := range rows[rowNumber] {
			if strings.TrimSpace(value) == "班级" {
				headerRow = rowNumber
				break
			}
		}
		if headerRow != 0 {
			break
		}
	}
	if headerRow == 0 {
		return Config{}, errors.New("未找到包含“班级”的字段标题行")
	}
	header := rows[headerRow]
	columns := make(map[string][2]int)
	for column := 0; column < 100; column++ {
		subjectID := importedSubjectID(header[column])
		if subjectID == "" {
			continue
		}
		if _, exists := columns[subjectID]; exists {
			continue
		}
		if strings.TrimSpace(header[column+1]) != "课时" {
			continue
		}
		columns[subjectID] = [2]int{column, column + 1}
	}
	if len(columns) == 0 {
		return Config{}, errors.New("未识别到课程教师和课时列")
	}
	config := defaultConfig()
	config.Classes = nil
	for rowNumber := headerRow + 1; rowNumber <= maxRow; rowNumber++ {
		values := rows[rowNumber]
		classID := strings.TrimSpace(values[1])
		if classID == "" {
			continue
		}
		if _, err := strconv.Atoi(classID); err != nil {
			// Some spreadsheet writers store a class label with a trailing 班.
			classID = strings.TrimSuffix(classID, "班")
		}
		if classID == "" {
			continue
		}
		grade := strings.TrimSpace(values[0])
		if grade == "" {
			grade = "示例年级"
		}
		headTeacher := strings.TrimSpace(values[3])
		assignments := make([]CourseAssignment, 0, len(config.Subjects))
		for _, subject := range config.Subjects {
			pair, present := columns[subject.ID]
			teacher := ""
			hoursValue := ""
			if present {
				teacher = strings.TrimSpace(values[pair[0]])
				hoursValue = values[pair[1]]
			}
			singleLessons, doubleBlocks, err := parseLessonValue(hoursValue)
			if err != nil {
				return Config{}, fmt.Errorf("%s 的%s课时格式错误: %w", classID, subject.Name, err)
			}
			if isTeacherlessSubject(subject.ID) {
				teacher = ""
			}
			assignments = append(assignments, CourseAssignment{SubjectID: subject.ID, Teacher: teacher, SingleLessons: singleLessons, DoubleBlocks: doubleBlocks})
		}
		config.Classes = append(config.Classes, ClassConfig{ID: classID, Name: classID + "班", Grade: grade, HeadTeacher: headTeacher, Assignments: assignments})
	}
	if len(config.Classes) == 0 {
		return Config{}, errors.New("未识别到班级数据")
	}
	return config, nil
}

func timeSlotsFromWorksheet(worksheet xlsxWorksheet, sharedStrings []string) ([]TimeSlot, error) {
	rows, maxRow, err := worksheetRows(worksheet, sharedStrings)
	if err != nil {
		return nil, err
	}
	required := []string{"时段安排", "开始时间", "结束时间", "排课属性"}
	headerRow := 0
	columns := make(map[string]int, len(required))
	for rowNumber := 1; rowNumber <= maxRow && rowNumber <= 20; rowNumber++ {
		candidate := make(map[string]int, len(required))
		for column, value := range rows[rowNumber] {
			value = strings.TrimSpace(value)
			for _, name := range required {
				if value == name {
					candidate[name] = column
				}
			}
		}
		if len(candidate) == len(required) {
			headerRow = rowNumber
			columns = candidate
			break
		}
	}
	if headerRow == 0 {
		return nil, errors.New("未找到“时段安排、开始时间、结束时间、排课属性”字段标题")
	}

	slots := make([]TimeSlot, 0, maxRow-headerRow)
	lessonIndex := 0
	for rowNumber := headerRow + 1; rowNumber <= maxRow; rowNumber++ {
		values := rows[rowNumber]
		name := strings.TrimSpace(values[columns["时段安排"]])
		startRaw := strings.TrimSpace(values[columns["开始时间"]])
		endRaw := strings.TrimSpace(values[columns["结束时间"]])
		property := strings.TrimSpace(values[columns["排课属性"]])
		if name == "" && startRaw == "" && endRaw == "" && property == "" {
			continue
		}
		if name == "" {
			return nil, fmt.Errorf("第 %d 行时段安排不能为空", rowNumber)
		}
		start, err := parseImportedTime(startRaw)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行开始时间无效: %w", rowNumber, err)
		}
		end, err := parseImportedTime(endRaw)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行结束时间无效: %w", rowNumber, err)
		}
		var schedulable bool
		switch property {
		case "可排课":
			schedulable = true
		case "不排课":
			schedulable = false
		default:
			return nil, fmt.Errorf("第 %d 行排课属性必须是“可排课”或“不排课”", rowNumber)
		}
		id := fmt.Sprintf("activity-%d", len(slots)+1)
		if schedulable {
			lessonIndex++
			id = fmt.Sprintf("p%d", lessonIndex)
		}
		slots = append(slots, TimeSlot{ID: id, Name: name, Start: start, End: end, Schedulable: schedulable})
	}
	if len(slots) == 0 {
		return nil, errors.New("未识别到任何作息时段")
	}
	return slots, nil
}

func parseImportedTime(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("时间不能为空")
	}
	if strings.Contains(raw, ":") {
		parts := strings.Split(raw, ":")
		if len(parts) != 2 && len(parts) != 3 {
			return "", fmt.Errorf("无法解析 %q", raw)
		}
		hour, hourErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		minute, minuteErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		second := 0
		var secondErr error
		if len(parts) == 3 {
			second, secondErr = strconv.Atoi(strings.TrimSpace(parts[2]))
		}
		if hourErr != nil || minuteErr != nil || secondErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 || second != 0 {
			return "", fmt.Errorf("无法解析 %q", raw)
		}
		return fmt.Sprintf("%02d:%02d", hour, minute), nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 || value >= 1 {
		return "", fmt.Errorf("无法解析 %q", raw)
	}
	minutes := int(math.Round(value * 24 * 60))
	if minutes < 0 || minutes >= 24*60 {
		return "", fmt.Errorf("无法解析 %q", raw)
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60), nil
}

func xlsxColumnIndex(reference string) (int, bool) {
	letters := ""
	for _, character := range reference {
		if character >= 'A' && character <= 'Z' {
			letters += string(character)
			continue
		}
		if character >= 'a' && character <= 'z' {
			letters += strings.ToUpper(string(character))
			continue
		}
		break
	}
	if letters == "" {
		return 0, false
	}
	column := 0
	for _, character := range letters {
		column = column*26 + int(character-'A'+1)
	}
	return column - 1, true
}

func xlsxCellValue(cell xlsxCell, sharedStrings []string) (string, error) {
	if cell.Type == "inlineStr" {
		return strings.Join(cell.Inline.Text, ""), nil
	}
	if cell.Type == "s" {
		index, err := strconv.Atoi(strings.TrimSpace(cell.Value))
		if err != nil || index < 0 || index >= len(sharedStrings) {
			return "", fmt.Errorf("共享字符串索引无效: %q", cell.Value)
		}
		return sharedStrings[index], nil
	}
	return strings.TrimSpace(cell.Value), nil
}

func importedSubjectID(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, " ", ""))
	switch name {
	case "语文":
		return "chinese"
	case "数学":
		return "math"
	case "英语":
		return "english"
	case "物理":
		return "physics"
	case "化学":
		return "chemistry"
	case "生物":
		return "biology"
	case "政治", "思品", "思想品德", "道法":
		return "morality"
	case "历史":
		return "history"
	case "地理":
		return "geography"
	case "音乐":
		return "music"
	case "美术":
		return "art"
	case "体育":
		return "pe"
	case "社团":
		return "club"
	case "劳技":
		return "labor"
	case "安全":
		return "safety"
	default:
		return ""
	}
}

func parseLessonValue(raw string) (int, int, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "＋", "+"))
	if raw == "" {
		return 0, 0, nil
	}
	parts := strings.Split(raw, "+")
	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("只支持普通课时或普通课时+连堂次数")
	}
	values := make([]int, len(parts))
	for index, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < 0 {
			return 0, 0, fmt.Errorf("无法解析 %q", raw)
		}
		values[index] = value
	}
	if len(values) == 1 {
		return values[0], 0, nil
	}
	return values[0], values[1], nil
}
