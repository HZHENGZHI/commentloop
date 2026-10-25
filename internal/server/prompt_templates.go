package server

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const defaultPromptTemplateID = "builtin-document-review"

type PromptTemplate struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Body              string    `json:"body"`
	AllowDocumentEdit bool      `json:"allowDocumentEdit"`
	AllowCodeRead     bool      `json:"allowCodeRead"`
	AllowCommentReply bool      `json:"allowCommentReply"`
	Builtin           bool      `json:"builtin"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type promptTemplateStore struct {
	db *sql.DB
}

func newPromptTemplateStore(db *sql.DB) *promptTemplateStore {
	return &promptTemplateStore{db: db}
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

func (s *promptTemplateStore) ensureDefaults() error {
	for _, item := range builtinPromptTemplates {
		now := formatTime(time.Now())
		_, err := s.db.Exec(`INSERT OR IGNORE INTO prompt_templates
			(id,name,description,body,allow_document_edit,allow_code_read,allow_comment_reply,builtin,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,1,?,?)`,
			item.ID, item.Name, item.Description, item.Body,
			boolInt(item.AllowDocumentEdit), boolInt(item.AllowCodeRead), boolInt(item.AllowCommentReply), now, now)
		if err != nil {
			return err
		}
	}
	// Repair built-ins seeded before capability columns were populated.
	var migrated string
	if err := s.db.QueryRow(`SELECT value FROM meta WHERE key='prompt_template_capabilities_v1'`).Scan(&migrated); err == sql.ErrNoRows {
		if _, err := s.db.Exec(`UPDATE prompt_templates SET allow_code_read=1, allow_comment_reply=1
			WHERE builtin=1 AND allow_code_read=0 AND allow_comment_reply=0`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO meta (key,value) VALUES ('prompt_template_capabilities_v1','1')`); err != nil {
			return err
		}
	}
	return nil
}

func (s *promptTemplateStore) list() ([]PromptTemplate, error) {
	rows, err := s.db.Query(`SELECT id,name,description,body,allow_document_edit,allow_code_read,allow_comment_reply,builtin,created_at,updated_at
		FROM prompt_templates ORDER BY builtin DESC, created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptTemplate{}
	for rows.Next() {
		var item PromptTemplate
		var allow, codeRead, commentReply, builtin int
		var created, updated string
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Body,
			&allow, &codeRead, &commentReply, &builtin, &created, &updated); err != nil {
			return nil, err
		}
		item.AllowDocumentEdit = allow != 0
		item.AllowCodeRead = codeRead != 0
		item.AllowCommentReply = commentReply != 0
		item.Builtin = builtin != 0
		item.CreatedAt = parseTime(created)
		item.UpdatedAt = parseTime(updated)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *promptTemplateStore) get(id string) (PromptTemplate, bool) {
	if strings.TrimSpace(id) == "" {
		id = defaultPromptTemplateID
	}
	var item PromptTemplate
	var allow, codeRead, commentReply, builtin int
	var created, updated string
	err := s.db.QueryRow(`SELECT id,name,description,body,allow_document_edit,allow_code_read,allow_comment_reply,builtin,created_at,updated_at
		FROM prompt_templates WHERE id=?`, id).
		Scan(&item.ID, &item.Name, &item.Description, &item.Body,
			&allow, &codeRead, &commentReply, &builtin, &created, &updated)
	if err != nil {
		return PromptTemplate{}, false
	}
	item.AllowDocumentEdit = allow != 0
	item.AllowCodeRead = codeRead != 0
	item.AllowCommentReply = commentReply != 0
	item.Builtin = builtin != 0
	item.CreatedAt = parseTime(created)
	item.UpdatedAt = parseTime(updated)
	return item, true
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
	_, err := s.db.Exec(`INSERT INTO prompt_templates
		(id,name,description,body,allow_document_edit,allow_code_read,allow_comment_reply,builtin,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,0,?,?)`,
		item.ID, item.Name, item.Description, item.Body, boolInt(allowEdit),
		boolInt(allowCodeRead), boolInt(allowCommentReply), formatTime(now), formatTime(now))
	if err != nil {
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
	_, err := s.db.Exec(`UPDATE prompt_templates SET name=?,description=?,body=?,
		allow_document_edit=?,allow_code_read=?,allow_comment_reply=?,updated_at=? WHERE id=?`,
		strings.TrimSpace(name), strings.TrimSpace(description), strings.TrimSpace(body),
		boolInt(allowEdit), boolInt(allowCodeRead), boolInt(allowCommentReply), formatTime(now), id)
	if err != nil {
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
	_, err := s.db.Exec(`DELETE FROM prompt_templates WHERE id=?`, id)
	return err
}

func (s *promptTemplateStore) reset(id string) (PromptTemplate, error) {
	for _, item := range builtinPromptTemplates {
		if item.ID != id {
			continue
		}
		now := time.Now()
		_, err := s.db.Exec(`UPDATE prompt_templates SET name=?,description=?,body=?,
			allow_document_edit=?,allow_code_read=?,allow_comment_reply=?,updated_at=? WHERE id=?`,
			item.Name, item.Description, item.Body, boolInt(item.AllowDocumentEdit),
			boolInt(item.AllowCodeRead), boolInt(item.AllowCommentReply), formatTime(now), id)
		if err != nil {
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
