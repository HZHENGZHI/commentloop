package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type checklistItemRow struct {
	ID        string `gorm:"type:text;primaryKey"`
	Path      string `gorm:"type:text;not null;index:idx_checklist_path"`
	Title     string `gorm:"type:text;not null"`
	Done      int    `gorm:"type:integer;not null;default:0"`
	Position  int    `gorm:"type:integer;not null;default:0"`
	Playbook  string `gorm:"type:text;not null;default:''"`
	CreatedAt string `gorm:"type:text;not null"`
	UpdatedAt string `gorm:"type:text;not null"`
}

func (checklistItemRow) TableName() string { return "doc_checklist_items" }

type checklistItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Done     bool   `json:"done"`
	Playbook string `json:"playbook,omitempty"`
}

type checklistPlaybook struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Items []string `json:"items"`
}

var builtinChecklists = []checklistPlaybook{
	{ID: "prd", Name: "PRD 评审", Items: []string{"目标与用户问题清晰", "范围与非目标明确", "验收标准可验证", "依赖和风险已列出", "数据与隐私影响已确认"}},
	{ID: "adr", Name: "ADR 评审", Items: []string{"背景和问题陈述清楚", "备选方案已比较", "决策与理由完整", "影响和迁移步骤明确", "回滚方案可行"}},
	{ID: "weekly", Name: "周报评审", Items: []string{"本周结果可量化", "风险和阻塞已说明", "下周计划可执行", "需要协调的事项已列出"}},
	{ID: "general", Name: "通用评审", Items: []string{"事实准确", "结构完整", "术语和口径一致", "下一步可执行"}},
}

func (s *Server) listChecklist(path string) ([]checklistItem, error) {
	var rows []checklistItemRow
	if err := s.db.Where("path = ?", path).Order("position ASC, created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]checklistItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, checklistItem{ID: row.ID, Title: row.Title, Done: row.Done != 0, Playbook: row.Playbook})
	}
	return out, nil
}

func (s *Server) handleGetChecklist(w http.ResponseWriter, r *http.Request) {
	ro, rel, err := s.resolveDocRef(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if s.rejectAgentDocPath(w, r, canonical, "当前任务只能读取关联文档") {
		return
	}
	items, err := s.listChecklist(canonical)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "playbooks": builtinChecklists})
}

type applyChecklistRequest struct {
	Path     string `json:"path"`
	Playbook string `json:"playbook"`
}

func (s *Server) handleApplyChecklist(w http.ResponseWriter, r *http.Request) {
	var req applyChecklistRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	var playbook *checklistPlaybook
	for i := range builtinChecklists {
		if builtinChecklists[i].ID == req.Playbook {
			playbook = &builtinChecklists[i]
			break
		}
	}
	if playbook == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "未知的检查清单"})
		return
	}
	existing := map[string]bool{}
	items, err := s.listChecklist(canonical)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, item := range items {
		existing[item.Title] = true
	}
	position := len(items)
	now := time.Now()
	for _, title := range playbook.Items {
		if existing[title] {
			continue
		}
		row := checklistItemRow{
			ID: "chk_" + commentID(), Path: canonical, Title: title,
			Position: position, Playbook: playbook.ID,
			CreatedAt: formatTime(now), UpdatedAt: formatTime(now),
		}
		if err := s.db.Create(&row).Error; err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		position++
	}
	s.writeChecklist(w, canonical)
}

type addChecklistRequest struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

func (s *Server) handleAddChecklistItem(w http.ResponseWriter, r *http.Request) {
	var req addChecklistRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "检查项不能为空或过长"})
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	var count int64
	if err := s.db.Model(&checklistItemRow{}).Where("path = ?", canonical).Count(&count).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now()
	row := checklistItemRow{
		ID: "chk_" + commentID(), Path: canonical, Title: req.Title,
		Position: int(count), CreatedAt: formatTime(now), UpdatedAt: formatTime(now),
	}
	if err := s.db.Create(&row).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.writeChecklist(w, canonical)
}

type toggleChecklistRequest struct {
	Path string `json:"path"`
	ID   string `json:"id"`
	Done bool   `json:"done"`
}

func (s *Server) handleToggleChecklistItem(w http.ResponseWriter, r *http.Request) {
	var req toggleChecklistRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	result := s.db.Model(&checklistItemRow{}).Where("id = ? AND path = ?", req.ID, canonical).
		Updates(map[string]any{"done": boolInt(req.Done), "updated_at": formatTime(time.Now())})
	if result.Error != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": result.Error.Error()})
		return
	}
	if result.RowsAffected == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "检查项不存在"})
		return
	}
	s.writeChecklist(w, canonical)
}

func (s *Server) handleDeleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	ro, rel, err := s.resolveDocRef(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	id := r.URL.Query().Get("id")
	result := s.db.Where("id = ? AND path = ?", id, canonical).Delete(&checklistItemRow{})
	if result.Error != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": result.Error.Error()})
		return
	}
	s.writeChecklist(w, canonical)
}

func (s *Server) writeChecklist(w http.ResponseWriter, path string) {
	items, err := s.listChecklist(path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "playbooks": builtinChecklists})
}

func (s *Server) checklistSummary(path string) string {
	items, err := s.listChecklist(path)
	if err != nil || len(items) == 0 {
		return ""
	}
	done := 0
	for _, item := range items {
		if item.Done {
			done++
		}
	}
	return fmt.Sprintf("%d/%d", done, len(items))
}
