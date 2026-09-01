package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
)

// buildScheduleWorkbook creates a self-contained workbook with a summary sheet
// and one worksheet per class. It intentionally uses OOXML directly so the
// server stays database-free and does not need a third-party runtime package.
func buildScheduleWorkbook(run ScheduleRun) ([]byte, error) {
	subjects := make(map[string]Subject, len(run.Config.Subjects))
	for _, subject := range run.Config.Subjects {
		subjects[subject.ID] = subject
	}
	styles := newWorkbookStyles(run.Config.Subjects)

	sheetNames := make([]string, 0, len(run.Config.Classes)+1)
	sheetNames = append(sheetNames, "汇总")
	usedNames := map[string]bool{"汇总": true}
	for _, classConfig := range run.Config.Classes {
		name := uniqueSheetName(classConfig.Name, usedNames)
		sheetNames = append(sheetNames, name)
	}

	schedules := make(map[string]ClassSchedule, len(run.Schedules))
	for _, schedule := range run.Schedules {
		schedules[schedule.ClassID] = schedule
	}

	files := make([]xlsxFile, 0, len(sheetNames)+5)
	files = append(files,
		xlsxFile{name: "[Content_Types].xml", data: contentTypesXML(len(sheetNames))},
		xlsxFile{name: "_rels/.rels", data: rootRelationshipsXML()},
		xlsxFile{name: "xl/workbook.xml", data: workbookXML(sheetNames)},
		xlsxFile{name: "xl/_rels/workbook.xml.rels", data: workbookRelationshipsXML(len(sheetNames))},
		xlsxFile{name: "xl/styles.xml", data: styles.xml()},
	)
	files = append(files, xlsxFile{name: "xl/worksheets/sheet1.xml", data: summaryWorksheetXML(run, styles)})
	for index, classConfig := range run.Config.Classes {
		files = append(files, xlsxFile{
			name: fmt.Sprintf("xl/worksheets/sheet%d.xml", index+2),
			data: classWorksheetXML(run, classConfig, schedules[classConfig.ID], subjects, styles),
		})
	}

	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, file := range files {
		writer, err := archive.Create(file.name)
		if err != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("创建工作簿文件失败: %w", err)
		}
		if _, err := writer.Write([]byte(file.data)); err != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("写入工作簿文件失败: %w", err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("完成工作簿失败: %w", err)
	}
	return output.Bytes(), nil
}

type xlsxFile struct {
	name string
	data string
}

type workbookStyles struct {
	styleBySubject map[string]int
	styleXML       string
}

func newWorkbookStyles(subjects []Subject) workbookStyles {
	fillIDs := make(map[string]int, len(subjects))
	borderIDs := make(map[string]int, len(subjects))
	for index, subject := range subjects {
		fillIDs[subject.ID] = 7 + index
		borderIDs[subject.ID] = 2 + index
	}

	var fills strings.Builder
	fills.WriteString(`<fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>`)
	for _, color := range []string{"F4F7FF", "5B8DEF", "EAF0F8", "EFF9F5", "FFF7DF"} {
		fills.WriteString(fmt.Sprintf(`<fill><patternFill patternType="solid"><fgColor rgb="FF%s"/><bgColor indexed="64"/></patternFill></fill>`, color))
	}
	for _, subject := range subjects {
		fills.WriteString(fmt.Sprintf(`<fill><patternFill patternType="solid"><fgColor rgb="FF%s"/><bgColor indexed="64"/></patternFill></fill>`, lightColor(subject.Color)))
	}

	var borders strings.Builder
	borders.WriteString(`<border><left/><right/><top/><bottom/><diagonal/></border>`)
	borders.WriteString(`<border><left style="thin"><color rgb="FFD9DEEA"/></left><right style="thin"><color rgb="FFD9DEEA"/></right><top style="thin"><color rgb="FFD9DEEA"/></top><bottom style="thin"><color rgb="FFD9DEEA"/></bottom><diagonal/></border>`)
	for _, subject := range subjects {
		color := normalizeColor(subject.Color)
		borders.WriteString(fmt.Sprintf(`<border><left style="medium"><color rgb="FF%s"/></left><right style="thin"><color rgb="FFD9DEEA"/></right><top style="thin"><color rgb="FFD9DEEA"/></top><bottom style="thin"><color rgb="FFD9DEEA"/></bottom><diagonal/></border>`, color))
	}

	var cellXfs strings.Builder
	cellXfs.WriteString(`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" applyAlignment="1"><alignment vertical="center"/></xf>`)
	cellXfs.WriteString(`<xf numFmtId="0" fontId="2" fillId="3" borderId="0" applyAlignment="1"><alignment horizontal="left" vertical="center"/></xf>`)
	cellXfs.WriteString(`<xf numFmtId="0" fontId="3" fillId="3" borderId="1" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf>`)
	cellXfs.WriteString(`<xf numFmtId="0" fontId="4" fillId="2" borderId="0" applyAlignment="1"><alignment horizontal="left" vertical="center" wrapText="1"/></xf>`)
	cellXfs.WriteString(`<xf numFmtId="0" fontId="1" fillId="2" borderId="1" applyAlignment="1"><alignment horizontal="left" vertical="center" wrapText="1"/></xf>`)
	cellXfs.WriteString(`<xf numFmtId="0" fontId="3" fillId="3" borderId="1" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf>`)
	cellXfs.WriteString(`<xf numFmtId="0" fontId="0" fillId="0" borderId="1" applyAlignment="1"><alignment horizontal="left" vertical="center" wrapText="1"/></xf>`)
	cellXfs.WriteString(`<xf numFmtId="0" fontId="0" fillId="0" borderId="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>`)
	cellXfs.WriteString(`<xf numFmtId="0" fontId="1" fillId="5" borderId="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf>`)

	styleBySubject := make(map[string]int, len(subjects))
	for index, subject := range subjects {
		styleID := 9 + index
		styleBySubject[subject.ID] = styleID
		cellXfs.WriteString(fmt.Sprintf(`<xf numFmtId="0" fontId="0" fillId="%d" borderId="%d" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf>`, fillIDs[subject.ID], borderIDs[subject.ID]))
	}

	stylesXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<fonts count="5">
<font><sz val="11"/><color rgb="FF252A3B"/><name val="Microsoft YaHei"/></font>
<font><b/><sz val="11"/><color rgb="FF252A3B"/><name val="Microsoft YaHei"/></font>
<font><b/><sz val="16"/><color rgb="FFFFFFFF"/><name val="Microsoft YaHei"/></font>
<font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Microsoft YaHei"/></font>
<font><sz val="10"/><color rgb="FF697386"/><name val="Microsoft YaHei"/></font>
</fonts>
<fills count="%d">%s</fills>
<borders count="%d">%s</borders>
<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>
<cellXfs count="%d">%s</cellXfs>
<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>
<dxfs count="0"/><tableStyles count="0" defaultTableStyle="TableStyleMedium2" defaultPivotStyle="PivotStyleMedium9"/>
</styleSheet>`, 7+len(subjects), fills.String(), 2+len(subjects), borders.String(), 9+len(subjects), cellXfs.String())

	return workbookStyles{styleBySubject: styleBySubject, styleXML: stylesXML}
}

func (styles workbookStyles) xml() string {
	return styles.styleXML
}

func summaryWorksheetXML(run ScheduleRun, styles workbookStyles) string {
	rows := make([]string, 0, len(run.Config.Classes)+7)
	rows = append(rows, worksheetRow(1, 30, inlineCell("A1", run.Name+" · 全部班级课表", 1)))
	rows = append(rows, worksheetRow(2, 24, inlineCell("A2", fmt.Sprintf("学校：%s    学期：%s    生成时间：%s    自检：%s", run.Config.SchoolName, run.Config.Semester, run.CreatedAt.Format("2006-01-02 15:04"), validationLabel(run.Validation)), 3)))
	rows = append(rows, worksheetRow(4, 24,
		inlineCell("A4", "班级", 2), inlineCell("B4", "班主任", 2), inlineCell("C4", "周课时", 2), inlineCell("D4", "教师数", 2), inlineCell("E4", "课表状态", 2), inlineCell("F4", "说明", 2)))
	for index, classConfig := range run.Config.Classes {
		row := index + 5
		rows = append(rows, worksheetRow(row, 24,
			inlineCell(cellRef("A", row), classConfig.Name, 6),
			inlineCell(cellRef("B", row), classConfig.HeadTeacher, 6),
			numberCell(cellRef("C", row), weeklyTotal(classConfig), 7),
			numberCell(cellRef("D", row), classTeacherCount(classConfig), 7),
			inlineCell(cellRef("E", row), validationLabel(run.Validation), 8),
			inlineCell(cellRef("F", row), "可切换到对应班级页查看详细课表", 6)))
	}
	return worksheetXML("A1:F"+fmt.Sprint(len(run.Config.Classes)+4), "A5", "A1:F1", "A2:F2", []string{"18", "18", "12", "12", "14", "34"}, rows)
}

func classWorksheetXML(run ScheduleRun, classConfig ClassConfig, schedule ClassSchedule, subjects map[string]Subject, styles workbookStyles) string {
	rows := make([]string, 0, len(run.Config.TimeSlots)+5)
	rows = append(rows, worksheetRow(1, 30, inlineCell("A1", run.Name+" · "+classConfig.Name, 1)))
	rows = append(rows, worksheetRow(2, 24, inlineCell("A2", fmt.Sprintf("学校：%s    学期：%s    班主任：%s    周课时：%d", run.Config.SchoolName, run.Config.Semester, classConfig.HeadTeacher, weeklyTotal(classConfig)), 3)))
	rows = append(rows, worksheetRow(4, 28, inlineCell("A4", "时段", 2)))
	for index, day := range run.Config.Days {
		rows[2] = strings.TrimSuffix(rows[2], "</row>") + inlineCell(cellRef(columnName(index+2), 4), day, 2) + "</row>"
	}

	cells := make(map[string]ScheduleCell, len(schedule.Cells))
	for _, cell := range schedule.Cells {
		cells[fmt.Sprintf("%d-%d", cell.Day, cell.Period)] = cell
	}
	slots := schedulableSlots(run.Config)
	for period, slot := range slots {
		row := period + 5
		rowCells := []string{inlineCell(cellRef("A", row), fmt.Sprintf("%s\n%s–%s", slot.Name, slot.Start, slot.End), 4)}
		for day := range run.Config.Days {
			cell, ok := cells[fmt.Sprintf("%d-%d", day, period)]
			if !ok {
				rowCells = append(rowCells, inlineCell(cellRef(columnName(day+2), row), "", 6))
				continue
			}
			subject := subjects[cell.SubjectID]
			styleID := styles.styleBySubject[cell.SubjectID]
			if styleID == 0 {
				styleID = 6
			}
			text := subject.Name + "\n" + cell.Teacher
			if cell.IsDouble {
				text += "\n连堂"
			}
			rowCells = append(rowCells, inlineCell(cellRef(columnName(day+2), row), text, styleID))
		}
		rows = append(rows, worksheetRow(row, 46, rowCells...))
	}
	return worksheetXML("A1:F"+fmt.Sprint(len(slots)+4), "A5", "A1:F1", "A2:F2", []string{"22", "22", "22", "22", "22", "22"}, rows)
}

func worksheetXML(dimension, freezeCell, titleMergeRef, metadataMergeRef string, widths []string, rows []string) string {
	var columns strings.Builder
	for index, width := range widths {
		columns.WriteString(fmt.Sprintf(`<col min="%d" max="%d" width="%s" customWidth="1"/>`, index+1, index+1, width))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<dimension ref="%s"/><sheetViews><sheetView showGridLines="0" workbookViewId="0"><pane ySplit="4" topLeftCell="%s" activePane="bottomLeft" state="frozen"/><selection pane="bottomLeft" activeCell="%s" sqref="%s"/></sheetView></sheetViews>
<sheetFormatPr defaultRowHeight="20"/><cols>%s</cols><sheetData>%s</sheetData><mergeCells count="2"><mergeCell ref="%s"/><mergeCell ref="%s"/></mergeCells><pageMargins left="0.3" right="0.3" top="0.5" bottom="0.5" header="0.2" footer="0.2"/>
</worksheet>`, dimension, freezeCell, freezeCell, freezeCell, columns.String(), strings.Join(rows, ""), titleMergeRef, metadataMergeRef)
}

func worksheetRow(row int, height int, cells ...string) string {
	return fmt.Sprintf(`<row r="%d" ht="%d" customHeight="1">%s</row>`, row, height, strings.Join(cells, ""))
}

func inlineCell(ref, value string, style int) string {
	return fmt.Sprintf(`<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, xmlText(value))
}

func numberCell(ref string, value, style int) string {
	return fmt.Sprintf(`<c r="%s" s="%d"><v>%d</v></c>`, ref, style, value)
}

func cellRef(column string, row int) string {
	return fmt.Sprintf("%s%d", column, row)
}

func columnName(index int) string {
	if index < 1 {
		return "A"
	}
	name := ""
	for index > 0 {
		index--
		name = string(rune('A'+index%26)) + name
		index /= 26
	}
	return name
}

func classTeacherCount(classConfig ClassConfig) int {
	teachers := make(map[string]bool)
	for _, assignment := range classConfig.Assignments {
		if strings.TrimSpace(assignment.Teacher) != "" && assignment.WeeklyLessons() > 0 {
			teachers[assignment.Teacher] = true
		}
	}
	return len(teachers)
}

func weeklyTotal(classConfig ClassConfig) int {
	total := 0
	for _, assignment := range classConfig.Assignments {
		total += assignment.WeeklyLessons()
	}
	return total
}

func validationLabel(report ValidationReport) string {
	if report.Passed {
		return "通过"
	}
	return "需检查"
}

func uniqueSheetName(raw string, used map[string]bool) string {
	invalid := strings.NewReplacer("/", "", "\\", "", "?", "", "*", "", "[", "", "]", "", ":", "")
	base := strings.TrimSpace(invalid.Replace(raw))
	if base == "" {
		base = "班级"
	}
	if len([]rune(base)) > 31 {
		base = string([]rune(base)[:31])
	}
	name := base
	for suffix := 2; used[name]; suffix++ {
		tail := fmt.Sprintf("-%d", suffix)
		limit := 31 - len([]rune(tail))
		name = string([]rune(base)[:minInt(len([]rune(base)), limit)]) + tail
	}
	used[name] = true
	return name
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalizeColor(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) == 3 {
		value = string([]byte{value[0], value[0], value[1], value[1], value[2], value[2]})
	}
	if len(value) != 6 {
		return "79808E"
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return "79808E"
		}
	}
	return strings.ToUpper(value)
}

func lightColor(value string) string {
	color := normalizeColor(value)
	var red, green, blue int
	_, _ = fmt.Sscanf(color, "%02X%02X%02X", &red, &green, &blue)
	blend := func(component int) int { return component + (255-component)*88/100 }
	return fmt.Sprintf("%02X%02X%02X", blend(red), blend(green), blend(blue))
}

func xmlText(value string) string {
	value = strings.Map(func(char rune) rune {
		if char == '\n' || char == '\r' || char == '\t' || char >= 0x20 {
			return char
		}
		return -1
	}, value)
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}

func contentTypesXML(sheetCount int) string {
	parts := []string{`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`, `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`, `<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`, `<Default Extension="xml" ContentType="application/xml"/>`, `<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`, `<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`}
	for index := 1; index <= sheetCount; index++ {
		parts = append(parts, fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, index))
	}
	parts = append(parts, `</Types>`)
	return strings.Join(parts, "")
}

func rootRelationshipsXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`
}

func workbookXML(sheetNames []string) string {
	var sheets strings.Builder
	for index, name := range sheetNames {
		sheets.WriteString(fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlText(name), index+1, index+1))
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><fileVersion appName="Microsoft Office Excel" lastEdited="7" lowestEdited="7" rupBuild="28000"/><workbookPr defaultThemeVersion="164011"/><bookViews><workbookView xWindow="0" yWindow="0" windowWidth="18000" windowHeight="12000"/></bookViews><sheets>%s</sheets><calcPr calcId="191029"/></workbook>`, sheets.String())
}

func workbookRelationshipsXML(sheetCount int) string {
	parts := []string{`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`}
	for index := 1; index <= sheetCount; index++ {
		parts = append(parts, fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, index, index))
	}
	parts = append(parts, fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`, sheetCount+1))
	return strings.Join(parts, "")
}

func exportFileName(runName string) string {
	name := strings.TrimSpace(runName)
	if name == "" {
		name = "chirepk"
	}
	name = strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", `"`, "-", "<", "-", ">", "-", "|", "-").Replace(name)
	name = strings.Trim(name, ". ")
	if name == "" {
		name = "chirepk"
	}
	return name + "-全部班级课表.xlsx"
}

func contentDispositionFilename(filename string) string {
	return fmt.Sprintf(`attachment; filename="chirepk-schedule.xlsx"; filename*=UTF-8''%s`, url.PathEscape(filename))
}
