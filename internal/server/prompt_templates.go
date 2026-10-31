package server

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultPromptTemplateID = "builtin-document-review"
	reviewPromptTemplateID  = "builtin-first-review"
)

func newPromptTemplateStore(db *gorm.DB) *promptTemplateStore {
	return &promptTemplateStore{db: db}
}

func (r promptTemplateRow) template() PromptTemplate {
	return PromptTemplate{
		ID: r.ID, Name: r.Name, Description: r.Description, Body: r.Body,
		AllowDocumentEdit: r.AllowDocumentEdit != 0,
		AllowCodeRead:     r.AllowCodeRead != 0,
		AllowCommentReply: r.AllowCommentReply != 0,
		Builtin:           r.Builtin != 0,
		CreatedAt:         parseTime(r.CreatedAt),
		UpdatedAt:         parseTime(r.UpdatedAt),
	}
}

func newPromptTemplateRow(item PromptTemplate) promptTemplateRow {
	return promptTemplateRow{
		ID: item.ID, Name: item.Name, Description: item.Description, Body: item.Body,
		AllowDocumentEdit: boolInt(item.AllowDocumentEdit),
		AllowCodeRead:     boolInt(item.AllowCodeRead),
		AllowCommentReply: boolInt(item.AllowCommentReply),
		Builtin:           boolInt(item.Builtin),
		CreatedAt:         formatTime(item.CreatedAt),
		UpdatedAt:         formatTime(item.UpdatedAt),
	}
}

func promptTemplateChanges(name, description, body string, allowEdit, allowCodeRead, allowCommentReply bool, now time.Time) map[string]any {
	return map[string]any{
		"name": name, "description": description, "body": body,
		"allow_document_edit": boolInt(allowEdit),
		"allow_code_read":     boolInt(allowCodeRead),
		"allow_comment_reply": boolInt(allowCommentReply),
		"updated_at":          formatTime(now),
	}
}

var builtinPromptTemplates = []PromptTemplate{
	{
		ID: "builtin-document-review", Name: "文档评审修改",
		Description:       "根据评论和讨论共识修改文档，保留无关内容。",
		AllowDocumentEdit: true,
		AllowCodeRead:     true,
		AllowCommentReply: true,
		Builtin:           true,
		Body:              `根据当前评论和讨论共识修改文档。只改与评论相关的内容，保持无关内容不变；回复时说明修改理由和必要的修改前后对照。`,
	},
	{
		ID: "builtin-first-review", Name: "首轮审阅",
		Description:       "通读全文，按严重级别提出审阅意见并生成审阅摘要；不修改文档。",
		AllowDocumentEdit: false,
		AllowCodeRead:     true,
		AllowCommentReply: true,
		Builtin:           true,
		Body:              `对整篇文档做一轮审阅，不要修改文档。重点检查：事实与引用、结构与逻辑、前后一致性、术语、遗漏的边界与风险、表达清晰度。最多给出 8 条评论，每条评论引用原文中最短且唯一的片段；按 blocking、should_fix、nit、question 标注严重级别。最后写一段审阅摘要：主要问题、建议处理顺序、可以忽略的项。`,
	},
	{
		ID: "builtin-requirements", Name: "需求拆解",
		Description:       "阅读讨论和文档，拆解目标、约束、验收标准和待确认问题。",
		AllowDocumentEdit: false,
		AllowCodeRead:     true,
		AllowCommentReply: true,
		Builtin:           true,
		Body:              `阅读当前文档、评论线程、已导入讨论和文档共识，拆解需求。输出：目标与范围、用户场景、约束、验收标准、待确认问题。不要修改文档，先把分析和不确定点回复给用户。`,
	},
	{
		ID: "builtin-bug-triage", Name: "Bug 排查",
		Description:       "阅读评论、文档和关联代码，先给出排查分析与修复方案。",
		AllowDocumentEdit: false,
		AllowCodeRead:     true,
		AllowCommentReply: true,
		Builtin:           true,
		Body:              `阅读当前文档、完整评论线程、相关讨论和关联代码，排查问题。输出：复现条件、可能原因与证据、验证步骤、建议修复方案。不要修改文档或代码，先回复分析结果；信息不足时明确说明。`,
	},
}

var builtinPromptTemplatesEN = map[string]PromptTemplate{
	"builtin-document-review": {
		Name:        "Document review",
		Description: "Edit the document according to the comment and document consensus while preserving unrelated content.",
		Body:        "Edit the document according to the current comment and document consensus. Change only content related to the comment and keep unrelated content unchanged. In your reply, explain the reasoning and include the necessary before/after excerpts.",
	},
	"builtin-first-review": {
		Name:        "First review",
		Description: "Read the whole document, leave severity-ranked review comments and a review summary without editing it.",
		Body:        "Review the whole document without editing it. Focus on facts and citations, structure and logic, consistency, terminology, missing edge cases and risks, and clarity. Leave at most 8 comments, each quoting the shortest unique passage; mark severity as blocking, should_fix, nit, or question. Finish with a review summary: main issues, suggested order, and items that can be ignored.",
	},
	"builtin-requirements": {
		Name:        "Requirements breakdown",
		Description: "Read discussions and the document, then break down goals, constraints, acceptance criteria, and open questions.",
		Body:        "Read the current document, comment thread, imported discussions, and document consensus, then break down the requirement. Output: goals and scope, user scenarios, constraints, acceptance criteria, and open questions. Do not edit the document; reply with the analysis and uncertainties first.",
	},
	"builtin-bug-triage": {
		Name:        "Bug investigation",
		Description: "Read comments, the document, and linked code; provide an investigation and a fix plan first.",
		Body:        "Read the current document, full comment thread, related discussions, and linked code, then investigate the issue. Output: reproduction conditions, likely causes and evidence, verification steps, and a proposed fix. Do not edit the document or code; reply with the analysis first, and state clearly when information is insufficient.",
	},
}

func localizePromptTemplate(item PromptTemplate, language string) PromptTemplate {
	if !item.Builtin || language != "en" {
		return item
	}
	translated, ok := builtinPromptTemplatesEN[item.ID]
	if !ok {
		return item
	}
	seeded, ok := seededBuiltinPromptTemplate(item.ID)
	if !ok {
		return item
	}
	// Only translate fields that still hold the seeded Chinese text. A user may
	// edit a built-in template; replacing that edit with the English default
	// would hide (and later overwrite) their customization.
	if item.Name == seeded.Name {
		item.Name = translated.Name
	}
	if item.Description == seeded.Description {
		item.Description = translated.Description
	}
	if item.Body == seeded.Body {
		item.Body = translated.Body
	}
	return item
}

func seededBuiltinPromptTemplate(id string) (PromptTemplate, bool) {
	for _, candidate := range builtinPromptTemplates {
		if candidate.ID == id {
			return candidate, true
		}
	}
	return PromptTemplate{}, false
}

func (s *promptTemplateStore) ensureDefaults() error {
	for _, item := range builtinPromptTemplates {
		now := time.Now()
		item.CreatedAt, item.UpdatedAt = now, now
		row := newPromptTemplateRow(item)
		if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}
	// Repair built-ins seeded before capability columns were populated.
	_, migrated, err := metaValue(s.db, "prompt_template_capabilities_v1")
	if err != nil || migrated {
		return nil
	}
	if err := s.db.Model(&promptTemplateRow{}).
		Where("builtin = 1 AND allow_code_read = 0 AND allow_comment_reply = 0").
		Updates(map[string]any{"allow_code_read": 1, "allow_comment_reply": 1}).Error; err != nil {
		return err
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&metaRow{Key: "prompt_template_capabilities_v1", Value: "1"}).Error
}

func (s *promptTemplateStore) list() ([]PromptTemplate, error) {
	var rows []promptTemplateRow
	if err := s.db.Order("builtin DESC, created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]PromptTemplate, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.template())
	}
	return out, nil
}

func (s *promptTemplateStore) get(id string) (PromptTemplate, bool) {
	if strings.TrimSpace(id) == "" {
		id = defaultPromptTemplateID
	}
	var row promptTemplateRow
	if err := s.db.Take(&row, "id = ?", id).Error; err != nil {
		return PromptTemplate{}, false
	}
	return row.template(), true
}

func (s *promptTemplateStore) create(name, description, body string, allowEdit, allowCodeRead, allowCommentReply bool) (PromptTemplate, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	body = strings.TrimSpace(body)
	if name == "" || body == "" {
		return PromptTemplate{}, fmt.Errorf("模板名称和内容不能为空")
	}
	if len([]rune(name)) > 100 || len([]rune(description)) > 500 || len(body) > 20000 {
		return PromptTemplate{}, fmt.Errorf("模板内容过长")
	}
	now := time.Now()
	item := PromptTemplate{
		ID: "pt_" + commentID(), Name: name, Description: description, Body: body,
		AllowDocumentEdit: allowEdit, AllowCodeRead: allowCodeRead,
		AllowCommentReply: allowCommentReply, CreatedAt: now, UpdatedAt: now,
	}
	row := newPromptTemplateRow(item)
	if err := s.db.Create(&row).Error; err != nil {
		return PromptTemplate{}, err
	}
	return item, nil
}

func (s *promptTemplateStore) update(id, name, description, body string, allowEdit, allowCodeRead, allowCommentReply bool) (PromptTemplate, error) {
	item, ok := s.get(id)
	if !ok {
		return PromptTemplate{}, sql.ErrNoRows
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(body) == "" {
		return PromptTemplate{}, fmt.Errorf("模板名称和内容不能为空")
	}
	if len([]rune(name)) > 100 || len([]rune(description)) > 500 || len(body) > 20000 {
		return PromptTemplate{}, fmt.Errorf("模板内容过长")
	}
	now := time.Now()
	changes := promptTemplateChanges(strings.TrimSpace(name), strings.TrimSpace(description), strings.TrimSpace(body),
		allowEdit, allowCodeRead, allowCommentReply, now)
	if err := s.db.Model(&promptTemplateRow{}).Where("id = ?", id).Updates(changes).Error; err != nil {
		return PromptTemplate{}, err
	}
	item.Name = strings.TrimSpace(name)
	item.Description = strings.TrimSpace(description)
	item.Body = strings.TrimSpace(body)
	item.AllowDocumentEdit = allowEdit
	item.AllowCodeRead = allowCodeRead
	item.AllowCommentReply = allowCommentReply
	item.UpdatedAt = now
	return item, nil
}

func (s *promptTemplateStore) delete(id string) error {
	item, ok := s.get(id)
	if !ok {
		return sql.ErrNoRows
	}
	if item.Builtin {
		return fmt.Errorf("内置模板不能删除，只能恢复默认")
	}
	return s.db.Delete(&promptTemplateRow{}, "id = ?", id).Error
}

func (s *promptTemplateStore) reset(id string) (PromptTemplate, error) {
	for _, item := range builtinPromptTemplates {
		if item.ID != id {
			continue
		}
		now := time.Now()
		changes := promptTemplateChanges(item.Name, item.Description, item.Body,
			item.AllowDocumentEdit, item.AllowCodeRead, item.AllowCommentReply, now)
		if err := s.db.Model(&promptTemplateRow{}).Where("id = ?", id).Updates(changes).Error; err != nil {
			return PromptTemplate{}, err
		}
		item.UpdatedAt = now
		return item, nil
	}
	return PromptTemplate{}, sql.ErrNoRows
}

func renderPromptTemplate(body string, vars map[string]string) string {
	for key, value := range vars {
		body = strings.ReplaceAll(body, "{{"+key+"}}", value)
	}
	return body
}
