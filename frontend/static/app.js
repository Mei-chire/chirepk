const state = {
  username: "",
  config: null,
  teachers: [],
  runs: [],
  preflight: null,
  activeView: "overview",
  currentClassId: "",
  currentRunId: "",
  currentScheduleClassId: "",
  queryRunId: "",
  queryTeacher: "",
  runDetails: new Map(),
  pollTimer: null,
  adjustmentData: null,
  selectedAdjustmentIndex: -1,
};

const viewMeta = {
  overview: ["Chirepk", "排课总览"],
  times: ["基础配置", "每日作息"],
  assignments: ["基础配置", "任课设置"],
  teachers: ["数据统计", "教师课时"],
  generate: ["自动排课", "开始排课"],
  records: ["自动排课", "排课记录"],
  schedule: ["排课记录", "课表详情"],
  query: ["课表核查", "教师查询"],
};

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => [...document.querySelectorAll(selector)];

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  if (response.status === 204) return null;
  const data = await response.json().catch(() => ({}));
  if (response.status === 401) showLogin(data.error || "登录状态已失效，请重新登录");
  if (!response.ok) throw new Error(data.error || `请求失败 (${response.status})`);
  return data;
}

function refreshIcons() {
  if (window.lucide) window.lucide.createIcons({ attrs: { "aria-hidden": "true" } });
}

function toast(message, type = "success") {
  const item = document.createElement("div");
  item.className = `toast ${type}`;
  item.innerHTML = `<i data-lucide="${type === "error" ? "circle-alert" : "circle-check"}"></i><span>${escapeHTML(message)}</span>`;
  $("#toastStack").append(item);
  refreshIcons();
  setTimeout(() => item.remove(), 3400);
}

function formatDate(value, includeTime = true) {
  if (!value) return "--";
  const date = new Date(value);
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit", day: "2-digit",
    ...(includeTime ? { hour: "2-digit", minute: "2-digit", hour12: false } : {}),
  }).format(date);
}

function statusLabel(status) {
  return ({ completed: "已完成", running: "排课中", queued: "等待中", failed: "未完成" })[status] || status;
}

function statusIcon(status) {
  return ({ completed: "circle-check", running: "loader-circle", queued: "clock-3", failed: "circle-x" })[status] || "circle";
}

function subjectById(id, config = state.config) {
  return config?.subjects.find((subject) => subject.id === id) || { id, name: id, color: "#79808e" };
}

function currentClass() {
  return state.config.classes.find((item) => item.id === state.currentClassId) || state.config.classes[0];
}

function renderClassPicker(query = "") {
  const classConfig = currentClass();
  if (!classConfig) return;
  $("#classPickerName").textContent = classConfig.name;
  $("#classPickerTeacher").textContent = `班主任 · ${classConfig.headTeacher || "未设置"}`;
  const normalized = query.trim().toLowerCase();
  const classes = state.config.classes.filter((item) => !normalized || `${item.name} ${item.headTeacher}`.toLowerCase().includes(normalized));
  $("#classPickerOptions").innerHTML = classes.length ? classes.map((item) => `<button type="button" class="class-picker-option ${item.id === state.currentClassId ? "selected" : ""}" data-action="select-class" data-class-id="${escapeHTML(item.id)}" role="option" aria-selected="${item.id === state.currentClassId}"><span><strong>${escapeHTML(item.name)}</strong><small>班主任 · ${escapeHTML(item.headTeacher || "未设置")}</small></span>${item.id === state.currentClassId ? `<i data-lucide="check"></i>` : ""}</button>`).join("") : `<div class="class-picker-empty">没有匹配的班级</div>`;
  refreshIcons();
}

function openClassPicker() {
  const picker = $("#classPicker");
  picker.classList.add("open");
  $("#classPickerMenu").hidden = false;
  $("#classPickerTrigger").setAttribute("aria-expanded", "true");
  $("#classPickerSearch").value = "";
  renderClassPicker();
  requestAnimationFrame(() => $("#classPickerSearch").focus());
}

function closeClassPicker() {
  const picker = $("#classPicker");
  if (!picker) return;
  picker.classList.remove("open");
  $("#classPickerMenu").hidden = true;
  $("#classPickerTrigger").setAttribute("aria-expanded", "false");
}

function initializeSmartSelect(element) {
  if (!element || element.dataset.ready === "true") return;
  const label = element.dataset.ariaLabel || "选择选项";
  const searchPlaceholder = element.dataset.searchPlaceholder || "";
  element.innerHTML = `<button type="button" class="smart-select-trigger" aria-haspopup="listbox" aria-expanded="false" aria-label="${escapeHTML(label)}">
    <span class="smart-select-mark"><i data-lucide="${escapeHTML(element.dataset.icon || "list-filter")}"></i></span>
    <span class="smart-select-value"><strong></strong><small></small></span>
    <i data-lucide="chevron-down" class="smart-select-chevron"></i>
  </button>
  <div class="smart-select-menu" hidden>
    <label class="smart-select-search" ${searchPlaceholder ? "" : "hidden"}><i data-lucide="search"></i><input placeholder="${escapeHTML(searchPlaceholder)}" autocomplete="off" aria-label="${escapeHTML(searchPlaceholder || "搜索选项")}"></label>
    <div class="smart-select-options" role="listbox" aria-label="${escapeHTML(label)}"></div>
  </div>`;
  element.dataset.ready = "true";
  element._smartOptions = [];
  element.querySelector(".smart-select-trigger").addEventListener("click", () => {
    if (element.classList.contains("open")) closeSmartSelect(element);
    else openSmartSelect(element);
  });
  element.querySelector(".smart-select-trigger").addEventListener("keydown", (event) => {
    if (event.key !== "ArrowDown") return;
    event.preventDefault();
    openSmartSelect(element);
  });
  element.querySelector(".smart-select-search input").addEventListener("input", (event) => renderSmartSelectOptions(element, event.target.value));
  element.querySelector(".smart-select-menu").addEventListener("click", (event) => {
    const option = event.target.closest("[data-smart-value]");
    if (!option) return;
    const previous = element.dataset.value || "";
    element.dataset.value = option.dataset.smartValue;
    renderSmartSelect(element);
    closeSmartSelect(element);
    if (previous !== element.dataset.value) element._smartOnChange?.(element.dataset.value);
  });
  element.querySelector(".smart-select-menu").addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      event.preventDefault();
      closeSmartSelect(element);
      element.querySelector(".smart-select-trigger").focus();
      return;
    }
    if (!["ArrowDown", "ArrowUp"].includes(event.key)) return;
    const options = [...element.querySelectorAll(".smart-select-option")];
    if (!options.length) return;
    event.preventDefault();
    const current = options.indexOf(document.activeElement);
    const next = event.key === "ArrowDown" ? (current + 1) % options.length : (current <= 0 ? options.length - 1 : current - 1);
    options[next].focus();
  });
}

function setSmartSelect(selector, { options, value = "", disabled = false, onChange } = {}) {
  const element = typeof selector === "string" ? $(selector) : selector;
  if (!element) return;
  initializeSmartSelect(element);
  element._smartOptions = (options || []).map((option) => ({
    ...option,
    value: String(option.value ?? ""),
    label: String(option.label ?? option.value ?? ""),
    description: String(option.description ?? ""),
  }));
  const requested = String(value ?? "");
  element.dataset.value = element._smartOptions.some((option) => option.value === requested) ? requested : (element._smartOptions[0]?.value || "");
  element._smartOnChange = onChange;
  element.querySelector(".smart-select-trigger").disabled = disabled || !element._smartOptions.length;
  renderSmartSelect(element);
}

function getSmartSelectValue(selector) {
  const element = typeof selector === "string" ? $(selector) : selector;
  return element?.dataset.value || "";
}

function renderSmartSelect(element) {
  const selected = element._smartOptions.find((option) => option.value === (element.dataset.value || ""));
  const placeholder = element.dataset.placeholder || "请选择";
  const value = element.querySelector(".smart-select-value");
  value.querySelector("strong").textContent = selected?.label || placeholder;
  const description = value.querySelector("small");
  description.textContent = selected?.description || "";
  description.hidden = !description.textContent;
  renderSmartSelectOptions(element, element.querySelector(".smart-select-search input").value);
}

function renderSmartSelectOptions(element, query = "") {
  const normalized = query.trim().toLowerCase();
  const options = element._smartOptions.filter((option) => !normalized || `${option.label} ${option.description} ${option.searchText || ""}`.toLowerCase().includes(normalized));
  const list = element.querySelector(".smart-select-options");
  list.innerHTML = options.length ? options.map((option) => `<button type="button" class="smart-select-option ${option.value === (element.dataset.value || "") ? "selected" : ""}" data-smart-value="${escapeHTML(option.value)}" role="option" aria-selected="${option.value === (element.dataset.value || "")}">
    ${option.color ? `<span class="smart-select-swatch" style="--option-color:${escapeHTML(option.color)}"></span>` : ""}
    <span class="smart-select-option-copy"><strong>${escapeHTML(option.label)}</strong>${option.description ? `<small>${escapeHTML(option.description)}</small>` : ""}</span>
    ${option.value === (element.dataset.value || "") ? `<i data-lucide="check"></i>` : ""}
  </button>`).join("") : `<div class="smart-select-empty">没有匹配的选项</div>`;
  refreshIcons();
}

function openSmartSelect(element) {
  if (element.querySelector(".smart-select-trigger").disabled) return;
  closeClassPicker();
  closeAllSmartSelects(element);
  element.classList.add("open");
  element.querySelector(".smart-select-menu").hidden = false;
  element.querySelector(".smart-select-trigger").setAttribute("aria-expanded", "true");
  const search = element.querySelector(".smart-select-search input");
  search.value = "";
  renderSmartSelectOptions(element);
  requestAnimationFrame(() => {
    if (!element.querySelector(".smart-select-search").hidden) search.focus();
    else (element.querySelector(".smart-select-option.selected") || element.querySelector(".smart-select-option"))?.focus();
  });
}

function closeSmartSelect(element) {
  if (!element?.classList.contains("open")) return;
  element.classList.remove("open");
  element.querySelector(".smart-select-menu").hidden = true;
  element.querySelector(".smart-select-trigger").setAttribute("aria-expanded", "false");
}

function closeAllSmartSelects(except) {
  $$(".smart-select.open").forEach((element) => {
    if (element !== except) closeSmartSelect(element);
  });
}

function weeklyTotal(classConfig) {
  return (classConfig?.assignments || []).reduce((sum, item) => sum + Number(item.singleLessons) + Number(item.doubleBlocks) * 2, 0);
}

function schedulableSlotCount(config = state.config) {
  return (config?.timeSlots || []).filter((slot) => slot.schedulable).length;
}

function weeklyCapacity(config = state.config) {
  return (config?.days?.length || 0) * schedulableSlotCount(config);
}

function gradeLabel(config = state.config) {
  const grades = [...new Set((config?.classes || []).map((item) => String(item.grade || "").trim()).filter(Boolean))];
  return grades.length === 1 ? grades[0] : (grades.length ? grades.join("、") : "待导入");
}

function classScopeLabel(config = state.config) {
  if (!state.preflight?.imported) return "任课数据待导入";
  const classes = config?.classes || [];
  if (!classes.length) return "任课数据待导入";
  const ordered = [...classes].sort((left, right) => left.id.localeCompare(right.id, "zh-CN", { numeric: true }));
  const range = ordered.length === 1 ? ordered[0].name : `${ordered[0].name}–${ordered[ordered.length - 1].name}`;
  return `${gradeLabel(config)} · ${range} · ${classes.length} 个班`;
}

function navigate(view) {
  if (!viewMeta[view]) return;
  closeClassPicker();
  closeAllSmartSelects();
  state.activeView = view;
  $$(".view").forEach((item) => item.classList.toggle("active", item.dataset.view === view));
  $$(".nav-item").forEach((item) => item.classList.toggle("active", item.dataset.viewTarget === view));
  $("#pageEyebrow").textContent = viewMeta[view][0];
  $("#pageTitle").textContent = viewMeta[view][1];
  $("#sidebar").classList.remove("open");
  if (view === "overview") renderOverview();
  if (view === "times") renderTimes();
  if (view === "assignments") renderAssignments();
  if (view === "teachers") renderTeachers();
  if (view === "generate") refreshPreflight();
  if (view === "records") renderRuns();
  if (view === "query") renderQuery();
  window.scrollTo({ top: 0, behavior: "smooth" });
  refreshIcons();
}

function renderOverview() {
  if (!state.config) return;
  const totalLessons = state.config.classes.reduce((sum, item) => sum + weeklyTotal(item), 0);
  const completed = state.runs.filter((run) => run.status === "completed").length;
  const imported = Boolean(state.preflight?.imported);
  $("#semesterLabel").textContent = state.config.semester;
  $("#schoolLabel").textContent = `${state.config.schoolName} · ${gradeLabel(state.config)}课程计划`;
  $("#updatedAt").textContent = `配置更新 ${formatDate(state.config.updatedAt)}`;
  $("#overviewMetrics").innerHTML = [
    ["班级数量", imported ? state.config.classes.length : "待导入", imported ? "个班级" : "上传后显示", "school", "blue"],
    ["任课教师", state.teachers.length, "位教师", "users", "coral"],
    ["本周课程", totalLessons, "班级课时", "notebook-tabs", "green"],
    ["有效课表", completed, "次生成记录", "badge-check", "yellow"],
  ].map(([label, value, unit, icon, tone]) => `<article class="metric-card"><div class="metric-top"><span>${label}</span><span class="metric-icon ${tone}"><i data-lucide="${icon}"></i></span></div><strong>${value}</strong><small>${unit}</small></article>`).join("");

  const recent = state.runs.slice(0, 4);
  $("#overviewRuns").innerHTML = recent.length ? recent.map((run) => `<div class="compact-run"><div><strong>${escapeHTML(run.name)}</strong><small>${escapeHTML(run.message)}</small></div><time>${formatDate(run.createdAt)}</time><span class="run-status ${run.status}"><i data-lucide="${statusIcon(run.status)}"></i>${statusLabel(run.status)}</span></div>`).join("") : emptyState("calendar-plus", "还没有排课记录");
  const periods = state.config.timeSlots.filter((slot) => slot.schedulable).length;
  $("#weekStructure").innerHTML = state.config.days.map((day) => `<div class="week-day"><strong>${escapeHTML(day.replace("星期", "周"))}</strong><span>${periods}</span><small>可排课时段</small></div>`).join("");
  renderWorkflow();
  refreshIcons();
}

function renderWorkflow() {
  const track = $("#workflowTrack");
  const chip = $("#workflowStatusChip");
  if (!track || !chip) return;
  const fallback = [
    { key: "import", label: "导入任课 Excel", done: false },
    { key: "check", label: "检查教师与课时", done: false },
    { key: "times", label: "完成每日作息", done: false },
    { key: "assignments", label: "保存任课设置", done: false },
    { key: "ready", label: "开始排课", done: false },
  ];
  const steps = state.preflight && state.preflight.steps && state.preflight.steps.length ? state.preflight.steps : fallback;
  const firstPending = steps.findIndex((step) => !step.done);
  const icons = { import: "file-up", check: "clipboard-check", times: "clock-3", assignments: "book-user", ready: "wand-sparkles" };
  const descriptions = {
    import: "读取班级、教师和课时",
    check: "确认课程结构可用",
    times: "设置五天作息与活动段",
    assignments: "保存每个班的任课信息",
    ready: "生成并自检全校课表",
  };
  track.innerHTML = steps.map((step, index) => {
    const status = step.done ? "done" : index === firstPending ? "active" : "locked";
    const icon = icons[step.key] || "circle";
    return '<div class="workflow-step ' + status + '"><div class="workflow-node"><i data-lucide="' + icon + '"></i></div><strong>' + escapeHTML(step.label) + '</strong><small>' + escapeHTML(descriptions[step.key] || "准备下一步") + '</small></div>';
  }).join("");
  const ready = Boolean(state.preflight && state.preflight.ready);
  chip.className = "workflow-status " + (ready ? "ready" : (state.preflight && state.preflight.imported ? "warning" : ""));
  chip.textContent = ready ? "配置完成，可以排课" : ((state.preflight && state.preflight.message) || "等待导入任课 Excel");
  refreshIcons();
}

function renderTimes() {
  if (!state.config.timeSlots.length) {
    $("#timeRows").innerHTML = `<tr><td colspan="6">${emptyState("calendar-x", "请先导入包含每日作息的 Excel，或新增安排")}</td></tr>`;
    updateCapacity();
    refreshIcons();
    return;
  }
  $("#timeRows").innerHTML = state.config.timeSlots.map((slot, index) => `<tr>
    <td><input data-time-index="${index}" data-time-field="name" value="${escapeHTML(slot.name)}" aria-label="安排名称"></td>
    <td><input type="time" data-time-index="${index}" data-time-field="start" value="${escapeHTML(slot.start)}" aria-label="开始时间"></td>
    <td><input type="time" data-time-index="${index}" data-time-field="end" value="${escapeHTML(slot.end)}" aria-label="结束时间"></td>
    <td><label class="switch"><input type="checkbox" data-time-index="${index}" data-time-field="schedulable" ${slot.schedulable ? "checked" : ""} aria-label="纳入排课"><span></span></label></td>
    <td><span class="slot-type ${slot.schedulable ? "lesson" : ""}">${slot.schedulable ? "课程" : "活动"}</span></td>
    <td class="action-column"><button class="row-action" data-action="delete-time" data-time-index="${index}" aria-label="删除${escapeHTML(slot.name)}" title="删除安排"><i data-lucide="trash-2"></i></button></td>
  </tr>`).join("");
  updateCapacity();
  refreshIcons();
}

function updateCapacity() {
  const count = schedulableSlotCount();
  const capacity = weeklyCapacity();
  const days = state.config?.days?.length || 0;
  const totals = (state.config.classes || []).map(weeklyTotal).filter((total) => total > 0);
  const plansMatch = totals.length > 0 && totals.every((total) => total === capacity);
  const valid = count > 0 && (!totals.length || plansMatch);
  const element = $("#capacityStatus");
  element.className = `inline-status ${valid ? "" : "warning"}`;
  const suffix = count === 0 ? "，请至少保留一个可排课时段" : (totals.length ? (plansMatch ? "，与当前课程计划一致" : "，请调整任课课时") : "，导入任课表后自动校验");
  element.innerHTML = `<i data-lucide="${valid ? "circle-check" : "circle-alert"}"></i><span>每天 ${count} 个排课时段，每周 ${days} 天，每班每周容量 ${capacity} 课时${suffix}</span>`;
  refreshIcons();
}

function renderAssignments() {
  const scope = $("#assignmentScopeLabel");
  if (scope) scope.textContent = classScopeLabel();
  const imported = Boolean(state.preflight?.imported);
  $("#addAssignmentBtn").disabled = !imported;
  $("#saveAssignmentsBtn").disabled = !imported;
  $("#headTeacherInput").disabled = !imported;
  $("#classPickerTrigger").disabled = !imported;
  if (!imported) {
    state.currentClassId = "";
    $("#classPickerName").textContent = "等待导入班级";
    $("#classPickerTeacher").textContent = "请先从首页导入任课 Excel";
    $("#headTeacherInput").value = "";
    $("#assignmentRows").innerHTML = '<tr><td colspan="6">' + emptyState("file-up", "请先从首页导入任课 Excel") + '</td></tr>';
    updateClassTotal();
    refreshIcons();
    return;
  }
  if (!state.currentClassId) state.currentClassId = state.config.classes[0]?.id || "";
  const classConfig = currentClass();
  if (!classConfig) {
    $("#classPickerName").textContent = "等待导入班级";
    $("#classPickerTeacher").textContent = "请先从首页导入任课 Excel";
    $("#headTeacherInput").value = "";
    $("#assignmentRows").innerHTML = '<tr><td colspan="6">' + emptyState("file-up", "请先从首页导入任课 Excel") + '</td></tr>';
    updateClassTotal();
    refreshIcons();
    return;
  }
  const assignments = classConfig.assignments || [];
  renderClassPicker();
  $("#headTeacherInput").value = classConfig.headTeacher;
  if (!assignments.length) {
    $("#assignmentRows").innerHTML = '<tr><td colspan="6">' + emptyState("file-up", "请先从首页导入任课 Excel") + '</td></tr>';
    updateClassTotal();
    refreshIcons();
    return;
  }
  $("#assignmentRows").innerHTML = assignments.map((assignment, index) => {
    const subject = subjectById(assignment.subjectId);
    const total = Number(assignment.singleLessons) + Number(assignment.doubleBlocks) * 2;
    const teacherless = ["club", "labor", "safety"].includes(assignment.subjectId);
    return `<tr>
      <td><span class="subject-name" style="--subject-color:${subject.color}"><span class="subject-swatch"></span>${escapeHTML(subject.name)}</span></td>
      <td><input data-assignment-index="${index}" data-assignment-field="teacher" value="${escapeHTML(assignment.teacher)}" aria-label="${escapeHTML(subject.name)}任课教师" ${teacherless ? 'disabled placeholder="无需设置"' : ""}></td>
      <td><input class="number-input" type="number" min="0" max="${Math.max(weeklyCapacity(), 1)}" data-assignment-index="${index}" data-assignment-field="singleLessons" value="${assignment.singleLessons}" aria-label="${escapeHTML(subject.name)}普通课时"></td>
      <td><input class="number-input" type="number" min="0" max="${Math.max(Math.floor(weeklyCapacity() / 2), 1)}" data-assignment-index="${index}" data-assignment-field="doubleBlocks" value="${assignment.doubleBlocks}" aria-label="${escapeHTML(subject.name)}连堂次数"></td>
      <td><span class="lesson-total" data-assignment-total="${index}">${total}<small>课时</small></span></td>
      <td class="action-column"><button class="row-action" data-action="delete-assignment" data-assignment-index="${index}" aria-label="删除${escapeHTML(subject.name)}" title="删除课程"><i data-lucide="trash-2"></i></button></td>
    </tr>`;
  }).join("");
  updateClassTotal();
  refreshIcons();
}

function updateClassTotal() {
  const total = weeklyTotal(currentClass());
  const capacity = weeklyCapacity();
  const complete = capacity > 0 && total === capacity;
  const element = $("#classWeeklyTotal");
  element.textContent = total;
  $("#classWeeklyCapacity").textContent = `/ ${capacity}`;
  element.style.color = complete ? "var(--green)" : "var(--coral)";
  const bar = $("#classTotalBar");
  bar.style.width = `${capacity ? Math.min(100, total / capacity * 100) : 0}%`;
  bar.style.background = complete ? "var(--green)" : "var(--coral)";
  const singles = $("#newCourseSingles");
  const doubles = $("#newCourseDoubles");
  if (singles) singles.max = String(Math.max(capacity, 1));
  if (doubles) doubles.max = String(Math.max(Math.floor(capacity / 2), 1));
}

function renderTeachers() {
  $("#teacherCount").textContent = state.teachers.length;
  const filter = $("#teacherSubjectFilter");
  const subjectSignature = state.config.subjects.map((item) => item.id).join("|");
  if (filter.dataset.signature !== subjectSignature) {
    const selected = getSmartSelectValue(filter);
    setSmartSelect(filter, {
      options: [{ value: "", label: "全部学科", description: "显示全部教师" }, ...state.config.subjects.map((item) => ({ value: item.name, label: item.name, description: "按学科筛选", color: item.color }))],
      value: selected,
      onChange: renderTeachers,
    });
    filter.dataset.signature = subjectSignature;
  }
  if (!state.teachers.length) {
    $("#teacherRows").innerHTML = '<tr><td colspan="5">' + emptyState("file-up", "请先从首页导入任课 Excel") + '</td></tr>';
    refreshIcons();
    return;
  }
  const query = $("#teacherSearch").value.trim().toLowerCase();
  const subject = getSmartSelectValue(filter);
  const filtered = state.teachers.filter((item) => {
    const matchesText = !query || `${item.teacher} ${item.classes.join(" ")}`.toLowerCase().includes(query);
    const matchesSubject = !subject || item.subjects.includes(subject);
    return matchesText && matchesSubject;
  });
  const maxLessons = Math.max(...state.teachers.map((item) => item.weeklyLessons), 1);
  $("#teacherRows").innerHTML = filtered.length ? filtered.map((item) => `<tr>
    <td><span class="teacher-name">${escapeHTML(item.teacher)}</span></td>
    <td><div class="hour-bar"><strong>${item.weeklyLessons}</strong><span style="--load:${Math.max(6, item.weeklyLessons / maxLessons * 100)}%"></span></div></td>
    <td><div class="tag-list">${item.subjects.map((value) => `<span class="tag">${escapeHTML(value)}</span>`).join("")}</div></td>
    <td>${item.classes.length} 个</td>
    <td>${escapeHTML(item.classes.join(" / "))}</td>
  </tr>`).join("") : `<tr><td colspan="5">${emptyState("search-x", "没有匹配的教师")}</td></tr>`;
}

async function refreshPreflight() {
  try {
    state.preflight = await api("/api/preflight");
    const panel = $("#preflightPanel");
    panel.className = `preflight ${state.preflight.ready ? "" : "error"}`;
    panel.innerHTML = `<i data-lucide="${state.preflight.ready ? "shield-check" : "shield-alert"}"></i><div><strong>${state.preflight.ready ? "数据检查通过" : "暂不能开始排课"}</strong><small>${escapeHTML(state.preflight.message)}${state.preflight.ready ? ` · ${state.preflight.classes} 个班 · ${state.preflight.lessons} 节课` : ""}</small></div>`;
    $("#startRunBtn").disabled = !state.preflight.ready || ["running", "queued"].includes(activeRun()?.status);
    renderWorkflow();
    refreshIcons();
  } catch (error) {
    toast(error.message, "error");
  }
}

function activeRun() {
  return state.runs.find((run) => ["running", "queued"].includes(run.status));
}

function renderRuns() {
  const container = $("#runGrid");
  if (!state.runs.length) {
    container.innerHTML = emptyState("archive", "还没有排课记录");
    refreshIcons();
    return;
  }
  container.innerHTML = state.runs.map((run) => `<article class="run-card ${run.status}">
    <div class="run-card-accent"></div><div class="run-card-body">
      <div class="run-card-top"><h3 title="${escapeHTML(run.name)}">${escapeHTML(run.name)}</h3><span class="run-status ${run.status}"><i data-lucide="${statusIcon(run.status)}"></i>${statusLabel(run.status)}</span></div>
      <div class="run-card-meta"><span><i data-lucide="calendar-clock"></i>创建于 ${formatDate(run.createdAt)}</span><span><i data-lucide="message-circle"></i>${escapeHTML(run.message)}</span></div>
      ${["running", "queued"].includes(run.status) ? `<div class="mini-progress"><div style="width:${run.progress}%"></div></div>` : ""}
    </div><div class="run-card-actions">
      <button data-action="view-run" data-run-id="${run.id}" ${run.status !== "completed" ? "disabled" : ""}><i data-lucide="table-2"></i>查看课表</button>
      <button data-action="delete-run" data-run-id="${run.id}" ${["running", "queued"].includes(run.status) ? "disabled" : ""}><i data-lucide="trash-2"></i>删除</button>
    </div></article>`).join("");
  refreshIcons();
}

async function openSchedule(runId) {
  try {
    const run = await getRunDetail(runId);
    state.currentRunId = runId;
    state.currentScheduleClassId = state.currentScheduleClassId || run.config.classes[0].id;
    if (!run.config.classes.some((item) => item.id === state.currentScheduleClassId)) state.currentScheduleClassId = run.config.classes[0].id;
    setSmartSelect("#scheduleClassSelect", {
      options: run.config.classes.map((item) => ({ value: item.id, label: item.name, description: `班主任 · ${item.headTeacher || "未设置"}` })),
      value: state.currentScheduleClassId,
      onChange: (value) => {
        state.currentScheduleClassId = value;
        renderSchedule(state.runDetails.get(state.currentRunId));
      },
    });
    $("#exportScheduleBtn").disabled = false;
    renderSchedule(run);
    navigate("schedule");
  } catch (error) {
    toast(error.message, "error");
  }
}

function renderSchedule(run) {
  const config = run.config;
  const classConfig = config.classes.find((item) => item.id === state.currentScheduleClassId) || config.classes[0];
  const schedule = run.schedules.find((item) => item.classId === classConfig.id);
  const slots = config.timeSlots.filter((slot) => slot.schedulable);
  const cells = new Map(schedule.cells.map((cell) => [`${cell.day}-${cell.period}`, cell]));
  $("#scheduleTitle").textContent = `${run.name} · ${classConfig.name}`;
  $("#scheduleMeta").textContent = `${config.semester} · 班主任 ${classConfig.headTeacher} · ${formatDate(run.createdAt)}`;
  $("#scheduleHead").innerHTML = `<tr><th>时段</th>${config.days.map((day) => `<th>${escapeHTML(day)}</th>`).join("")}</tr>`;
  $("#scheduleBody").innerHTML = slots.map((slot, period) => `<tr><td><div class="period-label"><strong>${escapeHTML(slot.name)}</strong><span>${slot.start}–${slot.end}</span></div></td>${config.days.map((_, day) => {
    const cell = cells.get(`${day}-${period}`);
    const subject = subjectById(cell.subjectId, config);
    return `<td><button type="button" class="lesson-chip" data-action="adjust-course" data-day="${day}" data-period="${period}" style="--subject-color:${subject.color}" aria-label="调整${escapeHTML(config.days[day])}${escapeHTML(slot.name)}${escapeHTML(subject.name)}"><strong>${escapeHTML(subject.name)}</strong><span>${escapeHTML(cell.teacher)}</span>${cell.isDouble ? `<em class="double-label">连堂</em>` : ""}<i class="lesson-edit-icon" data-lucide="replace"></i></button></td>`;
  }).join("")}</tr>`).join("");
  $("#validationStrip").innerHTML = run.validation.messages.map((message) => `<div class="validation-item"><i data-lucide="check"></i>${escapeHTML(message)}</div>`).join("");
  $("#schedulePassBadge").style.display = run.validation.passed ? "inline-flex" : "none";
  $("#undoScheduleBtn").disabled = !run.adjustments?.length;
  refreshIcons();
}

function schedulePositionLabel(config, positions) {
  const slots = config.timeSlots.filter((slot) => slot.schedulable);
  const first = positions[0];
  const last = positions.at(-1);
  if (!first) return "--";
  const day = config.days[first.day] || `第 ${first.day + 1} 天`;
  const firstSlot = slots[first.period]?.name || `第 ${first.period + 1} 节`;
  if (positions.length === 1) return `${day} · ${firstSlot}`;
  const lastSlot = slots[last.period]?.name || `第 ${last.period + 1} 节`;
  return `${day} · ${firstSlot}–${lastSlot}`;
}

function scheduleCellsLabel(config, cells) {
  const subjects = [...new Set(cells.map((cell) => subjectById(cell.subjectId, config).name))];
  const teachers = [...new Set(cells.map((cell) => cell.teacher).filter(Boolean))];
  return { subjects: subjects.join(" / "), teachers: teachers.join(" / ") || "无教师课程" };
}

function closeAdjustment() {
  const dialog = $("#adjustmentDialog");
  if (dialog.open) dialog.close();
  state.adjustmentData = null;
  state.selectedAdjustmentIndex = -1;
}

async function openAdjustment(day, period) {
  const run = state.runDetails.get(state.currentRunId);
  if (!run) return;
  const classConfig = run.config.classes.find((item) => item.id === state.currentScheduleClassId);
  const schedule = run.schedules.find((item) => item.classId === state.currentScheduleClassId);
  const cell = schedule?.cells.find((item) => item.day === day && item.period === period);
  if (!classConfig || !cell) return;
  const subject = subjectById(cell.subjectId, run.config);
  const dialog = $("#adjustmentDialog");
  state.adjustmentData = null;
  state.selectedAdjustmentIndex = -1;
  $("#adjustmentTitle").textContent = `调整 ${subject.name}`;
  $("#adjustmentSubtitle").textContent = classConfig.name;
  $("#adjustmentCurrent").innerHTML = `<span class="adjustment-current-mark" style="--subject-color:${subject.color}"></span><div><strong>${escapeHTML(subject.name)}${cell.isDouble ? " · 连堂" : ""}</strong><span>${escapeHTML(schedulePositionLabel(run.config, [{ day, period }]))} · ${escapeHTML(cell.teacher || "无教师课程")}</span></div>`;
  $("#candidateCount").textContent = "";
  $("#adjustmentCandidates").innerHTML = `<div class="adjustment-loading"><div><i data-lucide="loader-circle"></i><div>正在校验可交换时段</div></div></div>`;
  $("#adjustmentPreview").classList.add("hidden");
  $("#adjustmentConfirm").disabled = true;
  if (!dialog.open) dialog.showModal();
  refreshIcons();
  try {
    const query = new URLSearchParams({ classId: classConfig.id, day: String(day), period: String(period) });
    state.adjustmentData = await api(`/api/runs/${encodeURIComponent(run.id)}/adjustment-candidates?${query}`);
    renderAdjustmentCandidates(run);
  } catch (error) {
    $("#adjustmentCandidates").innerHTML = `<div class="adjustment-empty">${escapeHTML(error.message)}</div>`;
    refreshIcons();
  }
}

function renderAdjustmentCandidates(run) {
  const data = state.adjustmentData;
  if (!data) return;
  const source = scheduleCellsLabel(run.config, data.sourceCells);
  const sourceSubject = subjectById(data.sourceCells[0]?.subjectId, run.config);
  const isDouble = data.sourceCells.some((cell) => cell.isDouble);
  $("#adjustmentCurrent").innerHTML = `<span class="adjustment-current-mark" style="--subject-color:${sourceSubject.color}"></span><div><strong>${escapeHTML(source.subjects)}${isDouble ? " · 连堂" : ""}</strong><span>${escapeHTML(schedulePositionLabel(run.config, data.sourcePositions))} · ${escapeHTML(source.teachers)}</span></div>`;
  $("#candidateCount").textContent = `${data.candidates.length} 个`;
  if (!data.candidates.length) {
    $("#adjustmentCandidates").innerHTML = `<div class="adjustment-empty">当前没有满足全部硬约束的交换方案。</div>`;
    refreshIcons();
    return;
  }
  $("#adjustmentCandidates").innerHTML = data.candidates.map((candidate, index) => {
    const target = scheduleCellsLabel(run.config, candidate.targetCells);
    const selected = index === state.selectedAdjustmentIndex;
    return `<button type="button" class="candidate-option ${selected ? "selected" : ""}" data-action="select-adjustment" data-candidate-index="${index}" aria-pressed="${selected}"><span class="candidate-option-mark"><i data-lucide="calendar-sync"></i></span><span class="candidate-option-copy"><strong>${escapeHTML(schedulePositionLabel(run.config, candidate.targetPositions))}</strong><small>${escapeHTML(target.subjects)} · ${escapeHTML(target.teachers)}</small></span><span class="candidate-safe"><i data-lucide="shield-check"></i>可交换</span></button>`;
  }).join("");
  renderAdjustmentPreview(run);
  refreshIcons();
}

function selectAdjustment(index) {
  const run = state.runDetails.get(state.currentRunId);
  if (!run || !state.adjustmentData?.candidates[index]) return;
  state.selectedAdjustmentIndex = index;
  renderAdjustmentCandidates(run);
}

function renderAdjustmentPreview(run) {
  const preview = $("#adjustmentPreview");
  const candidate = state.adjustmentData?.candidates[state.selectedAdjustmentIndex];
  if (!candidate) {
    preview.classList.add("hidden");
    $("#adjustmentConfirm").disabled = true;
    return;
  }
  const source = scheduleCellsLabel(run.config, state.adjustmentData.sourceCells);
  const target = scheduleCellsLabel(run.config, candidate.targetCells);
  preview.innerHTML = `<div class="preview-route"><div><strong>${escapeHTML(source.subjects)}</strong><span>${escapeHTML(schedulePositionLabel(run.config, state.adjustmentData.sourcePositions))}</span></div><i data-lucide="repeat-2"></i><div><strong>${escapeHTML(target.subjects)}</strong><span>${escapeHTML(schedulePositionLabel(run.config, candidate.targetPositions))}</span></div></div><div class="constraint-checks"><span><i data-lucide="check"></i>教师无冲突</span><span><i data-lucide="check"></i>课时数不变</span><span><i data-lucide="check"></i>连堂结构完整</span><span><i data-lucide="check"></i>第九节配额符合</span><span><i data-lucide="check"></i>课程分布均匀</span></div>`;
  preview.classList.remove("hidden");
  $("#adjustmentConfirm").disabled = false;
}

function acceptUpdatedRun(run) {
  state.runDetails.set(run.id, run);
  const index = state.runs.findIndex((item) => item.id === run.id);
  if (index >= 0) {
    state.runs[index] = { ...state.runs[index], message: run.message, validation: run.validation, revision: run.revision };
  }
  renderSchedule(run);
  renderRuns();
  renderOverview();
}

async function applySelectedAdjustment() {
  const run = state.runDetails.get(state.currentRunId);
  const data = state.adjustmentData;
  const candidate = data?.candidates[state.selectedAdjustmentIndex];
  if (!run || !candidate) return;
  const button = $("#adjustmentConfirm");
  button.disabled = true;
  try {
    const updated = await api(`/api/runs/${encodeURIComponent(run.id)}/adjustments`, {
      method: "POST",
      body: JSON.stringify({
        classId: state.currentScheduleClassId,
        source: data.sourcePositions[0],
        target: candidate.targetPositions[0],
        expectedRevision: data.revision,
      }),
    });
    closeAdjustment();
    acceptUpdatedRun(updated);
    toast("课程已交换，全部硬约束校验通过");
  } catch (error) {
    closeAdjustment();
    state.runDetails.delete(run.id);
    try {
      acceptUpdatedRun(await getRunDetail(run.id));
    } catch (_) {}
    toast(error.message, "error");
  }
}

async function undoLastAdjustment() {
  const run = state.runDetails.get(state.currentRunId);
  if (!run?.adjustments?.length) return;
  if (!await confirmAction("撤销课程调整", "确认撤销最近一次课程交换吗？")) return;
  const button = $("#undoScheduleBtn");
  button.disabled = true;
  try {
    const updated = await api(`/api/runs/${encodeURIComponent(run.id)}/adjustments/undo`, {
      method: "POST", body: JSON.stringify({ expectedRevision: run.revision }),
    });
    acceptUpdatedRun(updated);
    toast("最近一次课程调整已撤销");
  } catch (error) {
    button.disabled = false;
    toast(error.message, "error");
  }
}

async function exportAllSchedules() {
  if (!state.currentRunId) {
    toast("请先打开一条已完成的课表记录", "error");
    return;
  }
  const button = $("#exportScheduleBtn");
  button.disabled = true;
  try {
    const response = await fetch(`/api/runs/${encodeURIComponent(state.currentRunId)}/export.xlsx`);
    if (!response.ok) {
      const data = await response.json().catch(() => ({}));
      if (response.status === 401) showLogin(data.error || "登录状态已失效，请重新登录");
      throw new Error(data.error || `导出失败 (${response.status})`);
    }
    const blob = await response.blob();
    const run = state.runDetails.get(state.currentRunId);
    const fileName = `${run?.name || "chirepk"}-全部班级课表.xlsx`.replace(/[\\/:*?"<>|]/g, "-");
    const objectURL = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = objectURL;
    anchor.download = fileName;
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(objectURL), 1200);
    toast("已导出全部班级课表");
  } catch (error) {
    toast(error.message, "error");
  } finally {
    button.disabled = false;
  }
}

async function renderQuery() {
  const completed = state.runs.filter((run) => run.status === "completed");
  if (!completed.length) {
    setSmartSelect("#queryRunSelect", { options: [{ value: "", label: "暂无已完成记录", description: "请先生成一份课表" }], value: "", disabled: true });
    setSmartSelect("#queryTeacherSelect", { options: [{ value: "", label: "暂无教师", description: "完成排课后可查询" }], value: "", disabled: true });
    $("#teacherWeek").innerHTML = emptyState("calendar-x", "完成一次排课后即可查询教师行程");
    refreshIcons();
    return;
  }
  if (!state.queryRunId || !completed.some((run) => run.id === state.queryRunId)) state.queryRunId = completed[0].id;
  setSmartSelect("#queryRunSelect", {
    options: completed.map((run) => ({ value: run.id, label: run.name, description: `${formatDate(run.createdAt)} · 已完成` })),
    value: state.queryRunId,
    onChange: (value) => {
      state.queryRunId = value;
      state.queryTeacher = "";
      renderQuery();
    },
  });
  try {
    const run = await getRunDetail(state.queryRunId);
    const teacherStats = new Map();
    run.schedules.flatMap((schedule) => schedule.cells).forEach((cell) => {
      if (!cell.teacher) return;
      const summary = teacherStats.get(cell.teacher) || { lessons: 0, subjects: new Set() };
      summary.lessons += 1;
      summary.subjects.add(subjectById(cell.subjectId, run.config).name);
      teacherStats.set(cell.teacher, summary);
    });
    const teachers = [...teacherStats.keys()].sort((a, b) => a.localeCompare(b, "zh-CN"));
    if (!state.queryTeacher || !teachers.includes(state.queryTeacher)) state.queryTeacher = teachers[0] || "";
    setSmartSelect("#queryTeacherSelect", {
      options: teachers.map((teacher) => {
        const summary = teacherStats.get(teacher);
        return { value: teacher, label: teacher, description: `${summary.lessons} 节 · ${[...summary.subjects].join("、")}` };
      }),
      value: state.queryTeacher,
      disabled: !teachers.length,
      onChange: (value) => {
        state.queryTeacher = value;
        renderTeacherWeek(run);
      },
    });
    renderTeacherWeek(run);
  } catch (error) {
    toast(error.message, "error");
  }
}

function renderTeacherWeek(run) {
  const config = run.config;
  const slots = config.timeSlots.filter((slot) => slot.schedulable);
  const classNames = new Map(config.classes.map((item) => [item.id, item.name]));
  const lessons = run.schedules.flatMap((schedule) => schedule.cells).filter((cell) => cell.teacher === state.queryTeacher);
  const grouped = new Map();
  let conflicts = 0;
  for (const lesson of lessons) {
    const key = `${lesson.day}-${lesson.period}`;
    if (grouped.has(key)) conflicts += 1;
    else grouped.set(key, lesson);
  }
  $("#queryConflictBadge").innerHTML = `<i data-lucide="${conflicts ? "shield-alert" : "shield-check"}"></i>冲突 ${conflicts} 项`;
  $("#queryConflictBadge").style.color = conflicts ? "#a44150" : "#1a745e";
  const dayColors = ["#5b8def", "#ff788a", "#28a680", "#f5bd45", "#8d72df"];
  const header = `<div class="teacher-matrix-head">节次与时间</div>${config.days.map((day, index) => `<div class="teacher-matrix-head day" style="--day-color:${dayColors[index]}">${escapeHTML(day)}</div>`).join("")}`;
  const rows = slots.map((slot, period) => {
    const periodCell = `<div class="teacher-period"><strong>${escapeHTML(slot.name)}</strong><span>${slot.start}–${slot.end}</span></div>`;
    const dayCells = config.days.map((_, dayIndex) => {
      const lesson = grouped.get(`${dayIndex}-${period}`);
      if (!lesson) return `<div class="teacher-slot empty">空闲</div>`;
      const subject = subjectById(lesson.subjectId, config);
      return `<div class="teacher-slot occupied" style="--subject-color:${subject.color}"><small>${slot.start}–${slot.end}</small><strong>${escapeHTML(classNames.get(lesson.classId))}</strong><span>${escapeHTML(subject.name)}</span>${lesson.isDouble ? `<span class="double-note">连堂</span>` : ""}</div>`;
    }).join("");
    return periodCell + dayCells;
  }).join("");
  $("#teacherWeek").innerHTML = `<div class="teacher-matrix">${header}${rows}</div>`;
  refreshIcons();
}

async function getRunDetail(id) {
  if (state.runDetails.has(id)) return state.runDetails.get(id);
  const run = await api(`/api/runs/${id}`);
  state.runDetails.set(id, run);
  return run;
}

async function saveConfig(successMessage, stage = "") {
  try {
    const suffix = stage ? "?stage=" + encodeURIComponent(stage) : "";
    state.config = await api("/api/config" + suffix, { method: "PUT", body: JSON.stringify(state.config) });
    [state.teachers, state.preflight] = await Promise.all([api("/api/teachers"), api("/api/preflight")]);
    toast(successMessage);
    renderOverview();
    renderWorkflow();
    if (state.activeView === "assignments") renderAssignments();
    if (state.activeView === "times") renderTimes();
  } catch (error) {
    toast(error.message, "error");
  }
}

async function importXlsxFile(file) {
  if (!file) return;
  const form = new FormData();
  form.append("file", file);
  const button = $("#importXlsxBtn");
  if (button) button.disabled = true;
  try {
    const response = await fetch("/api/import/xlsx", { method: "POST", body: form });
    const data = await response.json().catch(() => ({}));
    if (response.status === 401) showLogin(data.error || "登录状态已失效，请重新登录");
    if (!response.ok) throw new Error(data.error || ("导入失败 (" + response.status + ")"));
    [state.config, state.teachers, state.preflight] = await Promise.all([
      api("/api/config"), api("/api/teachers"), api("/api/preflight"),
    ]);
    state.runs = [];
    state.runDetails.clear();
    clearTimeout(state.pollTimer);
    state.pollTimer = null;
    state.currentRunId = "";
    state.currentScheduleClassId = "";
    state.queryRunId = "";
    state.queryTeacher = "";
    state.currentClassId = state.config.classes[0]?.id || "";
    renderOverview();
    renderTimes();
    renderAssignments();
    renderTeachers();
    renderRuns();
    navigate("generate");
    toast("已导入并自动保存 " + file.name + "，可以直接开始排课");
  } catch (error) {
    toast(error.message, "error");
  } finally {
    if (button) button.disabled = false;
    const input = $("#importXlsxInput");
    if (input) input.value = "";
  }
}

async function startRun() {
  try {
    const name = $("#runNameInput").value.trim();
    const run = await api("/api/runs", { method: "POST", body: JSON.stringify({ name }) });
    state.runs.unshift(run);
    $("#progressPanel").classList.remove("hidden");
    $("#startRunBtn").disabled = true;
    updateProgress(run);
    renderRuns();
    renderOverview();
    watchRun(run.id);
  } catch (error) {
    toast(error.message, "error");
  }
}

function updateProgress(run) {
  $("#progressValue").textContent = `${run.progress}%`;
  $("#progressMessage").textContent = run.message;
  $("#progressBar").style.width = `${run.progress}%`;
}

async function watchRun(runId) {
  clearTimeout(state.pollTimer);
  try {
    const run = await api(`/api/runs/${runId}`);
    updateProgress(run);
    const index = state.runs.findIndex((item) => item.id === runId);
    const summary = { ...run, config: undefined, schedules: undefined };
    if (index >= 0) state.runs[index] = summary;
    else state.runs.unshift(summary);
    renderRuns();
    renderOverview();
    if (["queued", "running"].includes(run.status)) {
      state.pollTimer = setTimeout(() => watchRun(runId), 650);
      return;
    }
    $("#startRunBtn").disabled = !state.preflight?.ready;
    if (run.status === "completed") {
      state.runDetails.set(run.id, run);
      toast("排课完成，全部约束已通过");
    } else {
      toast(run.message, "error");
    }
  } catch (error) {
    toast(error.message, "error");
    $("#startRunBtn").disabled = false;
  }
}

async function loadRuns() {
  state.runs = await api("/api/runs");
  const running = activeRun();
  if (running) {
    $("#progressPanel").classList.remove("hidden");
    updateProgress(running);
    watchRun(running.id);
  }
}

async function deleteRun(id) {
  const run = state.runs.find((item) => item.id === id);
  if (!run) return;
  const accepted = await confirmAction("删除排课记录", `“${run.name}”将从当前内存记录中移除。`);
  if (!accepted) return;
  try {
    await api(`/api/runs/${id}`, { method: "DELETE" });
    state.runs = state.runs.filter((item) => item.id !== id);
    state.runDetails.delete(id);
    renderRuns();
    renderOverview();
    toast("排课记录已删除");
  } catch (error) {
    toast(error.message, "error");
  }
}

function confirmAction(title, message) {
  const dialog = $("#confirmDialog");
  $("#confirmTitle").textContent = title;
  $("#confirmMessage").textContent = message;
  dialog.showModal();
  refreshIcons();
  return new Promise((resolve) => {
    const finish = (value) => {
      dialog.close();
      $("#confirmCancel").onclick = null;
      $("#confirmAccept").onclick = null;
      resolve(value);
    };
    $("#confirmCancel").onclick = () => finish(false);
    $("#confirmAccept").onclick = () => finish(true);
    dialog.oncancel = () => finish(false);
  });
}

function emptyState(icon, message) {
  return `<div class="empty-state"><div><i data-lucide="${icon}"></i><div>${escapeHTML(message)}</div></div></div>`;
}

function addMinutes(value, minutes) {
  const [hour, minute] = String(value || "00:00").split(":").map(Number);
  const total = (hour * 60 + minute + minutes) % (24 * 60);
  return `${String(Math.floor(total / 60)).padStart(2, "0")}:${String(total % 60).padStart(2, "0")}`;
}

function addTimeSlot() {
  const last = state.config.timeSlots.at(-1);
  const start = last?.end || "08:00";
  state.config.timeSlots.push({
    id: `custom-slot-${Date.now().toString(36)}`,
    name: "新安排",
    start,
    end: addMinutes(start, 40),
    schedulable: false,
  });
  renderTimes();
  const input = $("#timeRows tr:last-child input[data-time-field='name']");
  input?.focus();
  input?.select();
  toast("已新增一项安排，保存后生效");
}

async function deleteTimeSlot(index) {
  const slot = state.config.timeSlots[index];
  if (!slot) return;
  if (!await confirmAction("删除作息安排", `确认删除“${slot.name}”吗？保存作息后生效。`)) return;
  state.config.timeSlots.splice(index, 1);
  renderTimes();
  toast("安排已移除，保存后生效");
}

async function confirmTimeInput(input) {
  if (!input || input.dataset.confirming === "1") return;
  const slot = state.config.timeSlots[Number(input.dataset.timeIndex)];
  if (!slot) return;
  const field = input.dataset.timeField;
  const previous = input.dataset.before === undefined ? slot[field] : JSON.parse(input.dataset.before);
  const current = slot[field];
  if (previous === current) return;
  input.dataset.confirming = "1";
  const label = ({ name: "安排名称", start: "开始时间", end: "结束时间", schedulable: "排课状态" })[field];
  if (!await confirmAction("确认修改安排", `确认修改“${slot.name}”的${label}吗？`)) {
    slot[field] = previous;
  }
  delete input.dataset.confirming;
  renderTimes();
}

function openCourseDialog() {
  $("#courseForm").reset();
  $("#newCourseSingles").value = "0";
  $("#newCourseDoubles").value = "0";
  $("#courseDialog").showModal();
  $("#newCourseName").focus();
}

function closeCourseDialog() {
  $("#courseDialog").close();
}

function addCourseFromDialog() {
  const name = $("#newCourseName").value.trim();
  const teacher = $("#newCourseTeacher").value.trim();
  const singles = Math.max(0, Number($("#newCourseSingles").value || 0));
  const doubles = Math.max(0, Number($("#newCourseDoubles").value || 0));
  if (!name || !teacher) {
    toast("请填写课程名称和任课教师", "error");
    return;
  }
  const capacity = weeklyCapacity();
  if (weeklyTotal(currentClass()) + singles + doubles * 2 > capacity) {
	toast(`周总课时不能超过 ${capacity}，请先减少其他课程课时`, "error");
    return;
  }
  let subject = state.config.subjects.find((item) => item.name === name);
  if (subject && currentClass().assignments.some((item) => item.subjectId === subject.id)) {
    toast("当前班级已存在该课程", "error");
    return;
  }
  if (!subject) {
    const colors = ["#4f8fcf", "#d96576", "#2d9e83", "#a873d1", "#d99432", "#668c45"];
    subject = { id: `custom-${Date.now().toString(36)}`, name, color: colors[state.config.subjects.length % colors.length] };
    state.config.subjects.push(subject);
  }
  currentClass().assignments.push({ subjectId: subject.id, teacher, singleLessons: singles, doubleBlocks: doubles });
  closeCourseDialog();
  renderAssignments();
  toast(`${name}已添加，保存后生效`);
}

async function deleteAssignment(index) {
  const classConfig = currentClass();
  const assignment = classConfig.assignments[index];
  if (!assignment) return;
  const subject = subjectById(assignment.subjectId);
  if (!await confirmAction("删除课程", `确认从${classConfig.name}移除“${subject.name}”吗？`)) return;
  classConfig.assignments.splice(index, 1);
  const stillUsed = state.config.classes.some((item) => item.assignments.some((course) => course.subjectId === subject.id));
  if (!stillUsed && subject.id.startsWith("custom-")) {
    state.config.subjects = state.config.subjects.filter((item) => item.id !== subject.id);
  }
  renderAssignments();
  toast(`${subject.name}已移除，保存后生效`);
}

function setLoginMessage(message = "", type = "error") {
  const element = $("#loginMessage");
  element.textContent = message;
  element.hidden = !message;
  element.classList.toggle("success", type === "success");
}

function showLogin(message = "", type = "error") {
  clearTimeout(state.pollTimer);
  state.pollTimer = null;
  state.username = "";
  document.body.classList.remove("auth-pending", "auth-authenticated");
  document.body.classList.add("auth-guest");
  $("#appShell").setAttribute("aria-hidden", "true");
  $("#loginScreen").removeAttribute("aria-hidden");
  setLoginMessage(message, type);
  $$("dialog[open]").forEach((dialog) => dialog.close());
  requestAnimationFrame(() => $("#loginUsername").focus());
}

function showApplication(username) {
  state.username = username || "已登录用户";
  $("#currentUsername").textContent = state.username;
  document.body.classList.remove("auth-pending", "auth-guest");
  document.body.classList.add("auth-authenticated");
  $("#loginScreen").setAttribute("aria-hidden", "true");
  $("#appShell").removeAttribute("aria-hidden");
  setLoginMessage();
}

function setLoginBusy(busy) {
  const button = $("#loginSubmit");
  button.disabled = busy;
  button.innerHTML = busy
    ? `<span><i data-lucide="loader-circle"></i>正在登录</span><i data-lucide="arrow-right"></i>`
    : `<span><i data-lucide="log-in"></i>登录</span><i data-lucide="arrow-right"></i>`;
  refreshIcons();
}

async function loadApplication() {
  [state.config, state.teachers, state.preflight] = await Promise.all([
    api("/api/config"), api("/api/teachers"), api("/api/preflight"),
  ]);
  state.currentClassId = state.config.classes[0]?.id || "";
  await loadRuns();
  renderOverview();
  renderTimes();
  renderAssignments();
  renderTeachers();
  renderRuns();
  await refreshPreflight();
  navigate("overview");
}

async function login(event) {
  event.preventDefault();
  const username = $("#loginUsername").value;
  const password = $("#loginPassword").value;
  setLoginMessage();
  setLoginBusy(true);
  try {
    const response = await fetch("/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error || "登录失败，请重试");
    await loadApplication();
    showApplication(data.username);
    $("#loginPassword").value = "";
  } catch (error) {
    showLogin(error.message);
    $("#loginPassword").select();
  } finally {
    setLoginBusy(false);
  }
}

async function logout() {
  const button = $("#logoutBtn");
  button.disabled = true;
  try {
    await fetch("/api/auth/logout", { method: "POST" });
  } finally {
    state.config = null;
    state.teachers = [];
    state.runs = [];
    state.preflight = null;
    state.runDetails.clear();
    $("#loginForm").reset();
    showLogin("已退出当前账号", "success");
    button.disabled = false;
  }
}

function bindAuthEvents() {
  $("#loginForm").addEventListener("submit", login);
  $("#logoutBtn").addEventListener("click", logout);
  $("#passwordToggle").addEventListener("click", () => {
    const input = $("#loginPassword");
    const visible = input.type === "text";
    input.type = visible ? "password" : "text";
    const button = $("#passwordToggle");
    button.setAttribute("aria-label", visible ? "显示密码" : "隐藏密码");
    button.setAttribute("title", visible ? "显示密码" : "隐藏密码");
    button.innerHTML = `<i data-lucide="${visible ? "eye" : "eye-off"}"></i>`;
    refreshIcons();
    input.focus();
  });
}

function bindEvents() {
  document.addEventListener("click", (event) => {
    const target = event.target.closest("[data-view-target]");
    if (target) navigate(target.dataset.viewTarget);
    const action = event.target.closest("[data-action]");
    if (action?.dataset.action === "view-run") openSchedule(action.dataset.runId);
    if (action?.dataset.action === "delete-run") deleteRun(action.dataset.runId);
    if (action?.dataset.action === "delete-time") deleteTimeSlot(Number(action.dataset.timeIndex));
    if (action?.dataset.action === "delete-assignment") deleteAssignment(Number(action.dataset.assignmentIndex));
    if (action?.dataset.action === "adjust-course") openAdjustment(Number(action.dataset.day), Number(action.dataset.period));
    if (action?.dataset.action === "select-adjustment") selectAdjustment(Number(action.dataset.candidateIndex));
    if (action?.dataset.action === "select-class") {
      state.currentClassId = action.dataset.classId;
      closeClassPicker();
      renderAssignments();
    }
    if (!event.target.closest("#classPicker")) closeClassPicker();
    if (!event.target.closest(".smart-select")) closeAllSmartSelects();
  });
  $("#mobileMenu").addEventListener("click", () => $("#sidebar").classList.toggle("open"));
  $("#importXlsxBtn").addEventListener("click", () => $("#importXlsxInput").click());
  $("#importXlsxInput").addEventListener("change", (event) => importXlsxFile(event.target.files[0]));
  $("#addTimeSlotBtn").addEventListener("click", addTimeSlot);
  $("#saveTimesBtn").addEventListener("click", async () => {
    if (await confirmAction("保存作息修改", "确认将当前作息应用到星期一至星期五吗？")) saveConfig("作息设置已保存", "times");
  });
  $("#addAssignmentBtn").addEventListener("click", openCourseDialog);
  $("#saveAssignmentsBtn").addEventListener("click", async () => {
    if (await confirmAction("保存任课修改", `确认保存${currentClass().name}的任课信息吗？`)) saveConfig("任课信息已保存", "assignments");
  });
  $("#startRunBtn").addEventListener("click", startRun);
  $("#resetConfigBtn").addEventListener("click", async () => {
    if (!await confirmAction("清空当前配置", "每日作息、任课设置和教师统计都会清空，之后需要重新导入 Excel。")) return;
    try {
      state.config = await api("/api/config/reset", { method: "POST", body: "{}" });
      state.teachers = await api("/api/teachers");
      state.currentClassId = state.config.classes[0].id;
      state.runs = [];
      state.runDetails.clear();
      clearTimeout(state.pollTimer);
      state.pollTimer = null;
      state.preflight = await api("/api/preflight");
      renderTimes(); renderAssignments(); renderTeachers(); renderOverview(); renderWorkflow();
      toast("配置已清空，请重新导入 Excel");
    } catch (error) { toast(error.message, "error"); }
  });
  $("#timeRows").addEventListener("input", (event) => {
    const input = event.target.closest("[data-time-index]");
    if (!input) return;
    const slot = state.config.timeSlots[Number(input.dataset.timeIndex)];
    slot[input.dataset.timeField] = input.type === "checkbox" ? input.checked : input.value;
  });
  $("#timeRows").addEventListener("focusin", (event) => {
    const input = event.target.closest("[data-time-index]");
    if (!input) return;
    const slot = state.config.timeSlots[Number(input.dataset.timeIndex)];
    input.dataset.before = JSON.stringify(slot[input.dataset.timeField]);
  });
  $("#timeRows").addEventListener("focusout", async (event) => {
    const input = event.target.closest("[data-time-index]");
    if (input?.type !== "checkbox") confirmTimeInput(input);
  });
  $("#timeRows").addEventListener("change", (event) => {
    const input = event.target.closest("[data-time-index]");
    if (input?.type === "checkbox") confirmTimeInput(input);
  });
  $("#classPickerTrigger").addEventListener("click", () => {
    if ($("#classPicker").classList.contains("open")) closeClassPicker();
    else openClassPicker();
  });
  $("#classPickerSearch").addEventListener("input", (event) => renderClassPicker(event.target.value));
  $("#classPickerSearch").addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      closeClassPicker();
      $("#classPickerTrigger").focus();
    }
  });
  $("#headTeacherInput").addEventListener("input", (event) => { currentClass().headTeacher = event.target.value; });
  $("#assignmentRows").addEventListener("focusin", (event) => {
    const input = event.target.closest("[data-assignment-index][data-assignment-field='teacher']");
    if (input) input.dataset.before = JSON.stringify(currentClass().assignments[Number(input.dataset.assignmentIndex)].teacher);
  });
  $("#assignmentRows").addEventListener("input", (event) => {
    const input = event.target.closest("[data-assignment-index]");
    if (!input) return;
    const assignment = currentClass().assignments[Number(input.dataset.assignmentIndex)];
    const field = input.dataset.assignmentField;
    if (input.type === "number") {
      const previous = assignment[field];
      const value = Math.max(0, Number(input.value || 0));
      const multiplier = field === "doubleBlocks" ? 2 : 1;
      const proposedTotal = weeklyTotal(currentClass()) - previous * multiplier + value * multiplier;
      const capacity = weeklyCapacity();
      if (proposedTotal > capacity) {
        input.value = previous;
		toast(`周总课时已满 ${capacity}，不能继续增加`, "error");
        return;
      }
      assignment[field] = value;
    } else {
      assignment[field] = input.value;
    }
    const total = assignment.singleLessons + assignment.doubleBlocks * 2;
    const totalElement = document.querySelector(`[data-assignment-total="${input.dataset.assignmentIndex}"]`);
    totalElement.innerHTML = `${total}<small>课时</small>`;
    updateClassTotal();
  });
  $("#assignmentRows").addEventListener("focusout", async (event) => {
    const input = event.target.closest("[data-assignment-index][data-assignment-field='teacher']");
    if (!input) return;
    const assignment = currentClass().assignments[Number(input.dataset.assignmentIndex)];
    const previous = input.dataset.before === undefined ? assignment.teacher : JSON.parse(input.dataset.before);
    if (previous === assignment.teacher) return;
    const subject = subjectById(assignment.subjectId);
    if (!await confirmAction("确认修改任课教师", `确认将${currentClass().name}${subject.name}教师由“${previous || "未设置"}”改为“${assignment.teacher || "未设置"}”吗？`)) {
      assignment.teacher = previous;
      input.value = previous;
    } else {
      input.dataset.before = JSON.stringify(assignment.teacher);
    }
  });
  $("#courseCancel").addEventListener("click", closeCourseDialog);
  $("#courseDialog").addEventListener("cancel", (event) => { event.preventDefault(); closeCourseDialog(); });
  $("#courseForm").addEventListener("submit", (event) => { event.preventDefault(); addCourseFromDialog(); });
  $("#exportScheduleBtn").addEventListener("click", exportAllSchedules);
  $("#undoScheduleBtn").addEventListener("click", undoLastAdjustment);
  $("#adjustmentClose").addEventListener("click", closeAdjustment);
  $("#adjustmentCancel").addEventListener("click", closeAdjustment);
  $("#adjustmentConfirm").addEventListener("click", applySelectedAdjustment);
  $("#adjustmentDialog").addEventListener("cancel", (event) => { event.preventDefault(); closeAdjustment(); });
  $("#teacherSearch").addEventListener("input", renderTeachers);
}

async function init() {
  bindAuthEvents();
  bindEvents();
  refreshIcons();
  try {
    const response = await fetch("/api/auth/session");
    if (!response.ok) {
      showLogin();
      return;
    }
    const session = await response.json();
    await loadApplication();
    showApplication(session.username);
  } catch (error) {
    showLogin(`服务加载失败：${error.message}`);
  }
}

document.addEventListener("DOMContentLoaded", init);
