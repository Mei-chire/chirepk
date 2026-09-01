package main

import "time"

type teacherRow struct {
	Class, Head, Chinese, Math, English, Physics, Morality, History, Geography, Biology, PE, Music, Art string
}

func defaultConfig() Config {
	subjects := []Subject{
		{ID: "chinese", Name: "语文", Color: "#ff7a90"},
		{ID: "math", Name: "数学", Color: "#5b8def"},
		{ID: "english", Name: "英语", Color: "#9b72e7"},
		{ID: "morality", Name: "道法", Color: "#ef8f56"},
		{ID: "history", Name: "历史", Color: "#b17a55"},
		{ID: "geography", Name: "地理", Color: "#3ab49f"},
		{ID: "biology", Name: "生物", Color: "#6cad55"},
		{ID: "physics", Name: "物理", Color: "#4775c5"},
		{ID: "chemistry", Name: "化学", Color: "#8f70ad"},
		{ID: "music", Name: "音乐", Color: "#e96ca4"},
		{ID: "art", Name: "美术", Color: "#f2a93b"},
		{ID: "pe", Name: "体育", Color: "#31a7c7"},
		{ID: "club", Name: "社团", Color: "#e66f5b"},
		{ID: "labor", Name: "劳技", Color: "#7f9c47"},
		{ID: "safety", Name: "安全", Color: "#79808e"},
	}
	rows := []teacherRow{
		{"801", "王灿", "王灿", "谭璇", "张玲", "陈应", "石亚妮", "倪婷婷", "明文锋", "梁淑芬", "周潇潇", "梁美芳", "陈彤"},
		{"802", "晏宗星", "冯欣", "晏宗星", "肖珊珊", "陈应", "石亚妮", "倪婷婷", "明文锋", "崔云晓", "周潇潇", "梁美芳", "陈彤"},
		{"803", "王铭", "钟琳枝", "明婷婷", "舒树兵", "彭文刚", "刘睿", "罗婷婷", "石连芳", "崔云晓", "王铭", "梁美芳", "陈彤"},
		{"804", "明峰", "明峰", "周瑞柏", "宋亚男", "宋红艳", "明峰", "罗婷婷", "明文锋", "崔云晓", "王铭", "梁美芳", "陈彤"},
		{"805", "柯宝昌", "明依依", "柯丽钦", "柯宝昌", "宋红艳", "刘睿", "罗婷婷", "明文锋", "崔云晓", "王铭", "梁美芳", "陈彤"},
		{"806", "吴春晨", "胡泽芳", "吴春晨", "黎华云", "柯善华", "刘睿", "罗婷婷", "从志刚", "吴春晨", "周潇潇", "梁美芳", "陈彤"},
		{"807", "姜帆", "徐静", "梁明凯", "肖珊珊", "姜帆", "李志强", "倪婷婷", "从志刚", "石媛媛", "周潇潇", "梁美芳", "陈彤"},
		{"808", "刘媛媛", "刘媛媛", "李名锋", "柯宝昌", "明雪地", "刘媛媛", "黄琴琴", "从志刚", "石媛媛", "王铭", "梁美芳", "孙凤姣"},
		{"809", "喻红斌", "王灿", "喻红斌", "李照君", "柯善华", "李志强", "罗婷婷", "石连芳", "石媛媛", "周潇潇", "梁美芳", "孙凤姣"},
		{"810", "刘睿", "陈新琼", "刘睿", "黎华云", "陈应", "刘睿", "赵会兰", "从志刚", "崔云晓", "周潇潇", "梁美芳", "孙凤姣"},
		{"811", "黄航", "李儒才", "黄航", "邓晓双", "彭文刚", "明峰", "李宗贵", "明文锋", "黄航", "周潇潇", "张梦", "孙凤姣"},
		{"812", "张有锐", "骆琳", "张有锐", "柯曦", "明雪地", "刘媛媛", "罗婷婷", "张有锐", "崔云晓", "周潇潇", "张梦", "孙凤姣"},
		{"813", "李宗贵", "万佳佳", "明如倩", "李照君", "纪明卿", "刘睿", "李宗贵", "石连芳", "梁淑芬", "周潇潇", "张梦", "孙凤姣"},
		{"814", "纪明卿", "刘合华", "谭璇", "刘露", "纪明卿", "李志强", "李宗贵", "刘合华", "梁淑芬", "周潇潇", "张梦", "孙凤姣"},
		{"815", "梅光友", "黄智娟", "梅光友", "宋亚男", "肖瑞昌", "陈爱清", "倪婷婷", "石连芳", "梁淑芬", "周潇潇", "张梦", "孙凤姣"},
		{"816", "舒树兵", "宋亚亚", "周瑞富", "舒树兵", "肖瑞昌", "陈爱清", "罗婷婷", "宋亚亚", "石媛媛", "王铭", "张梦", "孙凤姣"},
		{"817", "万登淮", "石亚妮", "万登淮", "邓晓双", "潘际柱", "陈爱清", "曹正祥", "刘合华", "石媛媛", "王铭", "张梦", "孙凤姣"},
		{"818", "曹中平", "黄琴琴", "曹中平", "刘露", "骆才训", "陈爱清", "倪婷婷", "张有锐", "梁淑芬", "王铭", "张梦", "陈彤"},
		{"819", "袁野", "赵会兰", "袁野", "张玲", "姜帆", "陈爱清", "李宗贵", "石连芳", "梁淑芬", "王铭", "张梦", "陈彤"},
		{"820", "柯萌", "柯萌", "赵克雄", "骆锦思", "骆才训", "李志强", "李宗贵", "宋亚亚", "石媛媛", "王铭", "张梦", "陈彤"},
		{"821", "骆才训", "郑晓丽", "陈微微", "刘丽", "骆才训", "陈爱清", "李宗贵", "从志刚", "吴春晨", "王铭", "张梦", "陈彤"},
	}
	classes := make([]ClassConfig, 0, len(rows))
	for _, r := range rows {
		teachers := map[string]string{
			"chinese": r.Chinese, "math": r.Math, "english": r.English,
			"morality": r.Morality, "history": r.History, "geography": r.Geography,
			"biology": r.Biology, "physics": r.Physics, "chemistry": "",
			"music": r.Music, "art": r.Art, "pe": r.PE,
			"club": "", "labor": "", "safety": "",
		}
		hours := map[string][2]int{
			"chinese": {6, 0}, "math": {4, 3}, "english": {4, 2},
			"morality": {2, 0}, "history": {2, 0}, "geography": {3, 0},
			"biology": {3, 0}, "physics": {5, 0}, "chemistry": {0, 0},
			"music": {1, 0}, "art": {1, 0}, "pe": {1, 0}, "club": {2, 0},
			"labor": {1, 0}, "safety": {0, 0},
		}
		assignments := make([]CourseAssignment, 0, len(subjects))
		for _, s := range subjects {
			h := hours[s.ID]
			assignments = append(assignments, CourseAssignment{SubjectID: s.ID, Teacher: teachers[s.ID], SingleLessons: h[0], DoubleBlocks: h[1]})
		}
		classes = append(classes, ClassConfig{ID: r.Class, Name: r.Class + "班", Grade: "八年级", HeadTeacher: r.Head, Assignments: assignments})
	}
	return Config{
		Version: 1, SchoolName: "Chirepk 青禾中学", Semester: "2026年秋季学期",
		Days: []string{"星期一", "星期二", "星期三", "星期四", "星期五"},
		TimeSlots: []TimeSlot{
			{ID: "p1", Name: "第一节课", Start: "08:00", End: "08:40", Schedulable: true},
			{ID: "break", Name: "大课间", Start: "08:40", End: "09:10", Schedulable: false},
			{ID: "p2", Name: "第二节课", Start: "09:10", End: "09:50", Schedulable: true},
			{ID: "p3", Name: "第三节课", Start: "10:05", End: "10:45", Schedulable: true},
			{ID: "eye-am", Name: "眼保健操", Start: "10:45", End: "11:00", Schedulable: false},
			{ID: "p4", Name: "第四节课", Start: "11:00", End: "11:40", Schedulable: true},
			{ID: "lunch", Name: "中餐", Start: "11:40", End: "12:00", Schedulable: false},
			{ID: "rest", Name: "午休", Start: "12:40", End: "14:00", Schedulable: false},
			{ID: "p5", Name: "第五节课", Start: "14:20", End: "15:00", Schedulable: true},
			{ID: "p6", Name: "第六节课", Start: "15:10", End: "15:50", Schedulable: true},
			{ID: "eye-pm", Name: "眼保健操", Start: "15:50", End: "16:05", Schedulable: false},
			{ID: "p7", Name: "第七节课", Start: "16:05", End: "16:45", Schedulable: true},
			{ID: "p8", Name: "课后服务1", Start: "16:55", End: "17:30", Schedulable: true},
			{ID: "p9", Name: "课后服务2", Start: "17:40", End: "18:10", Schedulable: true},
		},
		Subjects: subjects, Classes: classes, UpdatedAt: time.Now(),
	}
}

// blankConfig keeps the reusable timetable and subject definitions while
// waiting for the user to import the authoritative teacher workbook.
func blankConfig() Config {
	config := defaultConfig()
	for index := range config.Classes {
		config.Classes[index].HeadTeacher = ""
		config.Classes[index].Assignments = make([]CourseAssignment, 0)
	}
	return config
}
