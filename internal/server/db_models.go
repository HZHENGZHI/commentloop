package server

// GORM models describing the state database schema. openStateDB uses them to
// add missing tables/columns/indexes, and every store queries through them.
// Column types and defaults mirror the historical hand-written DDL so that
// existing databases are left untouched.

type rootRow struct {
	Position int    `gorm:"type:integer;not null"`
	Kind     string `gorm:"type:text;not null"`
	Path     string `gorm:"type:text;not null"`
	Name     string `gorm:"type:text;not null;default:''"`
	Host     string `gorm:"type:text;not null;default:''"`
	Port     int    `gorm:"type:integer;not null;default:0"`
	Username string `gorm:"type:text;not null;default:''"`
	KeyPath  string `gorm:"type:text;not null;default:''"`
}

func (rootRow) TableName() string { return "roots" }

type commentRow struct {
	ID               string `gorm:"type:text;primaryKey"`
	Path             string `gorm:"type:text;not null;index:idx_comments_path"`
	Quote            string `gorm:"type:text;not null"`
	Anchor           string `gorm:"type:text;not null;default:''"`
	GroupID          string `gorm:"type:text;not null;default:'';index:idx_comments_group"`
	Prefix           string `gorm:"type:text;not null;default:''"`
	Suffix           string `gorm:"type:text;not null;default:''"`
	Line             int    `gorm:"type:integer;not null;default:0"`
	StartOffset      int    `gorm:"type:integer;not null;default:0"`
	EndOffset        int    `gorm:"type:integer;not null;default:0"`
	Resolved         int    `gorm:"type:integer;not null;default:0"`
	Orphaned         int    `gorm:"type:integer;not null;default:0"`
	CreatedAt        string `gorm:"type:text;not null"`
	UpdatedAt        string `gorm:"type:text;not null"`
	DiscussionIDs    string `gorm:"column:discussion_ids;type:text;not null;default:'[]'"`
	PromptTemplateID string `gorm:"type:text;not null;default:''"`
	Severity         string `gorm:"type:text;not null;default:''"`
	Reaction         string `gorm:"type:text;not null;default:''"`
	SuggestionQuote  string `gorm:"type:text;not null;default:''"`
	SuggestionText   string `gorm:"type:text;not null;default:''"`
	SuggestionMode   string `gorm:"type:text;not null;default:''"`
	SuggestionStatus string `gorm:"type:text;not null;default:''"`
}

func (commentRow) TableName() string { return "comments" }

type commentMessageRow struct {
	ID              string      `gorm:"type:text;primaryKey"`
	CommentID       string      `gorm:"type:text;not null;index:idx_comment_messages_comment"`
	Comment         *commentRow `gorm:"foreignKey:CommentID;references:ID;constraint:OnDelete:CASCADE"`
	Author          string      `gorm:"type:text;not null"`
	Body            string      `gorm:"type:text;not null"`
	BeforeText      string      `gorm:"type:text;not null;default:''"`
	AfterText       string      `gorm:"type:text;not null;default:''"`
	VersionID       string      `gorm:"type:text;not null;default:'';index:idx_comment_messages_version"`
	BeforeVersionID string      `gorm:"type:text;not null;default:''"`
	SectionStart    int         `gorm:"type:integer;not null;default:0"`
	SectionEnd      int         `gorm:"type:integer;not null;default:0"`
	CreatedAt       string      `gorm:"type:text;not null"`
}

func (commentMessageRow) TableName() string { return "comment_messages" }

type docVersionRow struct {
	ID        string `gorm:"type:text;primaryKey"`
	Path      string `gorm:"type:text;not null;uniqueIndex:idx_doc_versions_path_hash,priority:1;index:idx_doc_versions_path,priority:1"`
	Hash      string `gorm:"type:text;not null;uniqueIndex:idx_doc_versions_path_hash,priority:2"`
	Content   string `gorm:"type:text;not null"`
	CreatedAt string `gorm:"type:text;not null;index:idx_doc_versions_path,priority:2"`
}

func (docVersionRow) TableName() string { return "doc_versions" }

type codeRepoRow struct {
	ID           string `gorm:"type:text;primaryKey"`
	Path         string `gorm:"type:text;not null;unique"`
	Name         string `gorm:"type:text;not null"`
	Kind         string `gorm:"type:text;not null;default:'local'"`
	RemoteURL    string `gorm:"column:remote_url;type:text;not null;default:''"`
	RemoteBranch string `gorm:"type:text;not null;default:''"`
	CreatedAt    string `gorm:"type:text;not null"`
}

func (codeRepoRow) TableName() string { return "code_repos" }

type docCodeRepoRow struct {
	DocPath   string       `gorm:"type:text;not null;primaryKey;index:idx_doc_code_repos_doc"`
	RepoID    string       `gorm:"type:text;not null;primaryKey"`
	Repo      *codeRepoRow `gorm:"foreignKey:RepoID;references:ID;constraint:OnDelete:CASCADE"`
	CreatedAt string       `gorm:"type:text;not null"`
}

func (docCodeRepoRow) TableName() string { return "doc_code_repos" }

type docReviewRow struct {
	DocPath          string `gorm:"type:text;primaryKey"`
	Status           string `gorm:"type:text;not null;default:''"`
	Summary          string `gorm:"type:text;not null;default:''"`
	SummaryUpdatedAt string `gorm:"type:text;not null;default:''"`
	UpdatedAt        string `gorm:"type:text;not null"`
}

func (docReviewRow) TableName() string { return "doc_reviews" }

type metaRow struct {
	Key   string `gorm:"type:text;primaryKey"`
	Value string `gorm:"type:text;not null"`
}

func (metaRow) TableName() string { return "meta" }

type settingRow struct {
	Key   string `gorm:"type:text;primaryKey"`
	Value string `gorm:"type:text;not null"`
}

func (settingRow) TableName() string { return "settings" }

type agentSessionRow struct {
	ID          string `gorm:"type:text;primaryKey"`
	DocPath     string `gorm:"type:text;not null;index:idx_agent_sessions_doc,priority:1"`
	PiSessionID string `gorm:"column:pi_session_id;type:text;not null"`
	Title       string `gorm:"type:text;not null"`
	CreatedAt   string `gorm:"type:text;not null;index:idx_agent_sessions_doc,priority:2"`
	UpdatedAt   string `gorm:"type:text;not null"`
}

func (agentSessionRow) TableName() string { return "agent_sessions" }

type docAgentActiveRow struct {
	DocPath   string `gorm:"type:text;primaryKey"`
	SessionID string `gorm:"type:text;not null"`
	UpdatedAt string `gorm:"type:text;not null"`
}

func (docAgentActiveRow) TableName() string { return "doc_agent_active" }

type docDiscussionRow struct {
	ID           string `gorm:"type:text;primaryKey"`
	DocPath      string `gorm:"type:text;not null;uniqueIndex:idx_doc_discussions_doc_source,priority:1;index:idx_doc_discussions_doc,priority:1"`
	Title        string `gorm:"type:text;not null"`
	SourcePath   string `gorm:"type:text;not null;uniqueIndex:idx_doc_discussions_doc_source,priority:2"`
	SourceKind   string `gorm:"type:text;not null"`
	Agent        string `gorm:"type:text;not null;default:'pi'"`
	Cwd          string `gorm:"type:text;not null;default:''"`
	SessionID    string `gorm:"type:text;not null;default:''"`
	MessageCount int    `gorm:"type:integer;not null;default:0"`
	SourceMtime  int64  `gorm:"type:integer;not null;default:0"`
	SourceSize   int64  `gorm:"type:integer;not null;default:0"`
	CreatedAt    string `gorm:"type:text;not null"`
	ImportedAt   string `gorm:"type:text;not null;index:idx_doc_discussions_doc,priority:2"`
}

func (docDiscussionRow) TableName() string { return "doc_discussions" }

type discussionMessageRow struct {
	ID           string            `gorm:"type:text;primaryKey"`
	DiscussionID string            `gorm:"type:text;not null;uniqueIndex:idx_discussion_messages_discussion_seq,priority:1;index:idx_discussion_messages_discussion,priority:1"`
	Discussion   *docDiscussionRow `gorm:"foreignKey:DiscussionID;references:ID;constraint:OnDelete:CASCADE"`
	Seq          int               `gorm:"type:integer;not null;uniqueIndex:idx_discussion_messages_discussion_seq,priority:2;index:idx_discussion_messages_discussion,priority:2"`
	Role         string            `gorm:"type:text;not null"`
	Body         string            `gorm:"type:text;not null"`
	CreatedAt    string            `gorm:"type:text;not null;default:''"`
}

func (discussionMessageRow) TableName() string { return "discussion_messages" }

type promptTemplateRow struct {
	ID                string `gorm:"type:text;primaryKey"`
	Name              string `gorm:"type:text;not null"`
	Description       string `gorm:"type:text;not null;default:''"`
	Body              string `gorm:"type:text;not null"`
	AllowDocumentEdit int    `gorm:"type:integer;not null;default:1"`
	AllowCodeRead     int    `gorm:"type:integer;not null;default:1"`
	AllowCommentReply int    `gorm:"type:integer;not null;default:1"`
	Builtin           int    `gorm:"type:integer;not null;default:0"`
	CreatedAt         string `gorm:"type:text;not null"`
	UpdatedAt         string `gorm:"type:text;not null"`
}

func (promptTemplateRow) TableName() string { return "prompt_templates" }

type agentJobRow struct {
	ID               string `gorm:"type:text;primaryKey"`
	DocPath          string `gorm:"type:text;not null"`
	CommentID        string `gorm:"type:text;not null"`
	AgentType        string `gorm:"type:text;not null;index:idx_agent_jobs_queue,priority:2"`
	Prompt           string `gorm:"type:text;not null"`
	PromptTemplateID string `gorm:"type:text;not null;default:''"`
	Capabilities     string `gorm:"type:text;not null;default:'{}'"`
	RepoIDs          string `gorm:"column:repo_ids;type:text;not null;default:'[]'"`
	Token            string `gorm:"type:text;not null"`
	Status           string `gorm:"type:text;not null;index:idx_agent_jobs_queue,priority:1"`
	RunnerID         string `gorm:"type:text;not null;default:''"`
	LeaseUntil       string `gorm:"type:text;not null;default:''"`
	Error            string `gorm:"type:text;not null;default:''"`
	CreatedAt        string `gorm:"type:text;not null;index:idx_agent_jobs_queue,priority:3"`
	StartedAt        string `gorm:"type:text;not null;default:''"`
	FinishedAt       string `gorm:"type:text;not null;default:''"`
}

func (agentJobRow) TableName() string { return "agent_jobs" }

type agentAuditRow struct {
	ID        string `gorm:"type:text;primaryKey"`
	JobID     string `gorm:"type:text;not null;default:''"`
	Token     string `gorm:"type:text;not null;default:''"`
	Tool      string `gorm:"type:text;not null;default:''"`
	Method    string `gorm:"type:text;not null"`
	Path      string `gorm:"type:text;not null"`
	Allowed   int    `gorm:"type:integer;not null;default:0"`
	Reason    string `gorm:"type:text;not null;default:''"`
	CreatedAt string `gorm:"type:text;not null"`
}

func (agentAuditRow) TableName() string { return "agent_audit" }

type docConsensusRow struct {
	DocPath   string `gorm:"type:text;primaryKey"`
	Content   string `gorm:"type:text;not null"`
	Source    string `gorm:"type:text;not null;default:''"`
	Version   int    `gorm:"type:integer;not null;default:1"`
	UpdatedAt string `gorm:"type:text;not null"`
}

func (docConsensusRow) TableName() string { return "doc_consensus" }

// stateModels is the AutoMigrate order: parents before children.
var stateModels = []any{
	&rootRow{}, &commentRow{}, &commentMessageRow{}, &docVersionRow{},
	&codeRepoRow{}, &docCodeRepoRow{}, &metaRow{}, &settingRow{},
	&agentSessionRow{}, &docAgentActiveRow{}, &docDiscussionRow{},
	&discussionMessageRow{}, &promptTemplateRow{}, &agentJobRow{},
	&agentAuditRow{}, &docConsensusRow{}, &docReviewRow{},
}
