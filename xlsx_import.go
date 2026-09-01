package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
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

// importConfigFromXLSX reads the first visible worksheet and maps the fixed
// course columns used by the school's 任课课时汇总表 into the app's domain model.
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
	var selected xlsxSheet
	for _, sheet := range workbook.Sheets.Items {
		if sheet.State != "hidden" && sheet.State != "veryHidden" {
			selected = sheet
			break
		}
	}
	if selected.RelID == "" {
		return Config{}, errors.New("XLSX 中没有可读取的工作表")
	}
	relsBytes, err := readZipEntry(files, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return Config{}, errors.New("XLSX 缺少工作表关系信息")
	}
	var rels xlsxRelationships
	if err := xml.Unmarshal(relsBytes, &rels); err != nil {
		return Config{}, fmt.Errorf("工作表关系格式错误: %w", err)
	}
	target := ""
	for _, relationship := range rels.Items {
		if relationship.ID == selected.RelID {
			target = relationship.Target
			break
		}
	}
	if target == "" {
		return Config{}, errors.New("找不到第一个工作表")
	}
	target = strings.TrimPrefix(target, "/")
	if !strings.HasPrefix(target, "xl/") {
		target = path.Join("xl", target)
	}
	worksheetBytes, err := readZipEntry(files, target)
	if err != nil {
		return Config{}, errors.New("找不到工作表数据")
	}
	sharedStrings, err := readSharedStrings(files)
	if err != nil {
		return Config{}, err
	}
	var worksheet xlsxWorksheet
	if err := xml.Unmarshal(worksheetBytes, &worksheet); err != nil {
		return Config{}, fmt.Errorf("工作表格式错误: %w", err)
	}
	return configFromWorksheet(worksheet, sharedStrings)
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

func configFromWorksheet(worksheet xlsxWorksheet, sharedStrings []string) (Config, error) {
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
				return Config{}, err
			}
			cells[column] = value
		}
		rows[number] = cells
	}
	header := rows[2]
	if len(header) == 0 {
		return Config{}, errors.New("未找到第 2 行字段标题")
	}
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
	for rowNumber := 3; rowNumber <= maxRow; rowNumber++ {
		values := rows[rowNumber]
		classID := strings.TrimSpace(values[1])
		if classID == "" {
			continue
		}
		if _, err := strconv.Atoi(classID); err != nil {
			// Some spreadsheet writers store a class label as "801班".
			classID = strings.TrimSuffix(classID, "班")
		}
		if classID == "" {
			continue
		}
		grade := strings.TrimSpace(values[0])
		if grade == "" {
			grade = "八年级"
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

func isTeacherlessSubject(subjectID string) bool {
	switch subjectID {
	case "club", "labor", "safety":
		return true
	default:
		return false
	}
}
