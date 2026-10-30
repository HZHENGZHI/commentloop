package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type AgentJob struct {
	ID               string            `json:"id"`
	DocPath          string            `json:"docPath"`
	CommentID        string            `json:"commentId"`
	AgentType        string            `json:"agentType"`
	Prompt           string            `json:"prompt"`
	PromptTemplateID string            `json:"promptTemplateId,omitempty"`
	Capabilities     agentCapabilities `json:"capabilities"`
	RepoIDs          []string          `json:"repoIds,omitempty"`
	Token            string            `json:"-"`
	Status           string            `json:"status"`
	RunnerID         string            `json:"runnerId,omitempty"`
	LeaseUntil       time.Time         `json:"leaseUntil,omitempty"`
	Error            string            `json:"error,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	StartedAt        time.Time         `json:"startedAt,omitempty"`
	FinishedAt       time.Time         `json:"finishedAt,omitempty"`
}

type agentJobStore struct {
	db *gorm.DB
}

func newAgentJobStore(db *gorm.DB) *agentJobStore { return &agentJobStore{db: db} }

func newAgentToken() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return "job_" + hex.EncodeToString(b[:])
}

func (s *agentJobStore) create(job AgentJob) (AgentJob, error) {
	now := time.Now()
	job.ID = "aj_" + commentID()
	job.Token = newAgentToken()
	job.Status = "queued"
	job.CreatedAt = now
	caps, _ := json.Marshal(job.Capabilities)
	repoIDs, _ := json.Marshal(job.RepoIDs)
	err := s.db.Create(&agentJobRow{
		ID: job.ID, DocPath: job.DocPath, CommentID: job.CommentID, AgentType: job.AgentType,
		Prompt: job.Prompt, PromptTemplateID: job.PromptTemplateID,
		Capabilities: string(caps), RepoIDs: string(repoIDs), Token: job.Token,
		Status: job.Status, CreatedAt: formatTime(now),
	}).Error
	if err != nil {
		return AgentJob{}, err
	}
	return job, nil
}

func (r agentJobRow) job() AgentJob {
	job := AgentJob{
		ID: r.ID, DocPath: r.DocPath, CommentID: r.CommentID, AgentType: r.AgentType,
		Prompt: r.Prompt, PromptTemplateID: r.PromptTemplateID, Token: r.Token,
		Status: r.Status, RunnerID: r.RunnerID, Error: r.Error,
		LeaseUntil: parseTime(r.LeaseUntil), CreatedAt: parseTime(r.CreatedAt),
		StartedAt: parseTime(r.StartedAt), FinishedAt: parseTime(r.FinishedAt),
	}
	_ = json.Unmarshal([]byte(r.Capabilities), &job.Capabilities)
	_ = json.Unmarshal([]byte(r.RepoIDs), &job.RepoIDs)
	return job
}

func (s *agentJobStore) claim(agentType, runnerID string, lease time.Duration) (AgentJob, bool, error) {
	var job AgentJob
	claimed := false
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row agentJobRow
		err := tx.Select("id", "doc_path", "comment_id", "agent_type", "prompt", "prompt_template_id",
			"capabilities", "repo_ids", "token").
			Where("status = ? AND agent_type = ?", "queued", agentType).
			Order("created_at, id").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		job = row.job()
		job.Status = "running"
		job.RunnerID = runnerID
		job.StartedAt = now
		job.LeaseUntil = now.Add(lease)
		if err := tx.Model(&agentJobRow{}).Where("id = ? AND status = ?", job.ID, "queued").
			Updates(map[string]any{
				"status": "running", "runner_id": runnerID,
				"started_at": formatTime(now), "lease_until": formatTime(job.LeaseUntil),
			}).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	if err != nil || !claimed {
		return AgentJob{}, false, err
	}
	return job, true, nil
}

func (s *agentJobStore) get(id string) (AgentJob, bool) {
	var row agentJobRow
	if err := s.db.Take(&row, "id = ?", id).Error; err != nil {
		return AgentJob{}, false
	}
	return row.job(), true
}

func (s *agentJobStore) finish(id, runnerID string, success bool, message string) error {
	now := time.Now()
	status := "error"
	if success {
		status = "done"
	}
	return s.updateRunning(id, runnerID, map[string]any{
		"status": status, "error": message, "finished_at": formatTime(now), "lease_until": "",
	})
}

// updateRunning changes a running job only on behalf of the runner that owns it.
func (s *agentJobStore) updateRunning(id, runnerID string, changes map[string]any) error {
	res := s.db.Model(&agentJobRow{}).
		Where("id = ? AND runner_id = ? AND status = ?", id, runnerID, "running").Updates(changes)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("任务不存在或不属于该 Runner")
	}
	return nil
}

func (s *agentJobStore) heartbeat(id, runnerID string, lease time.Duration) error {
	return s.updateRunning(id, runnerID, map[string]any{"lease_until": formatTime(time.Now().Add(lease))})
}

func (s *agentJobStore) recent(limit int) ([]AgentJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []agentJobRow
	if err := s.db.Omit("token").Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AgentJob, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.job())
	}
	return out, nil
}

func (s *agentJobStore) audit(jobID, token, tool, method, path string, allowed bool, reason string) {
	_ = s.db.Create(&agentAuditRow{
		ID: "aa_" + commentID(), JobID: jobID, Token: token, Tool: tool, Method: method, Path: path,
		Allowed: boolInt(allowed), Reason: reason, CreatedAt: formatTime(time.Now()),
	}).Error
}
