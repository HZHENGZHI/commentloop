package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
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
	db *sql.DB
}

func newAgentJobStore(db *sql.DB) *agentJobStore { return &agentJobStore{db: db} }

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
	_, err := s.db.Exec(`INSERT INTO agent_jobs
		(id,doc_path,comment_id,agent_type,prompt,prompt_template_id,capabilities,repo_ids,token,status,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.DocPath, job.CommentID, job.AgentType, job.Prompt, job.PromptTemplateID,
		string(caps), string(repoIDs), job.Token, job.Status, formatTime(now))
	if err != nil {
		return AgentJob{}, err
	}
	return job, nil
}

func (s *agentJobStore) claim(agentType, runnerID string, lease time.Duration) (AgentJob, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return AgentJob{}, false, err
	}
	defer tx.Rollback()
	var job AgentJob
	var caps, repoIDs string
	var created string
	err = tx.QueryRow(`SELECT id,doc_path,comment_id,agent_type,prompt,prompt_template_id,capabilities,repo_ids,token
		FROM agent_jobs WHERE status='queued' AND agent_type=? ORDER BY created_at,id LIMIT 1`, agentType).
		Scan(&job.ID, &job.DocPath, &job.CommentID, &job.AgentType, &job.Prompt,
			&job.PromptTemplateID, &caps, &repoIDs, &job.Token)
	if err == sql.ErrNoRows {
		return AgentJob{}, false, nil
	}
	if err != nil {
		return AgentJob{}, false, err
	}
	_ = json.Unmarshal([]byte(caps), &job.Capabilities)
	_ = json.Unmarshal([]byte(repoIDs), &job.RepoIDs)
	now := time.Now()
	_ = created
	job.Status = "running"
	job.RunnerID = runnerID
	job.StartedAt = now
	job.LeaseUntil = now.Add(lease)
	if _, err := tx.Exec(`UPDATE agent_jobs SET status='running',runner_id=?,started_at=?,lease_until=?
		WHERE id=? AND status='queued'`, runnerID, formatTime(now), formatTime(job.LeaseUntil), job.ID); err != nil {
		return AgentJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AgentJob{}, false, err
	}
	return job, true, nil
}

func (s *agentJobStore) get(id string) (AgentJob, bool) {
	var job AgentJob
	var caps, repoIDs, lease, created, started, finished string
	err := s.db.QueryRow(`SELECT id,doc_path,comment_id,agent_type,prompt,prompt_template_id,capabilities,repo_ids,
		token,status,runner_id,lease_until,error,created_at,started_at,finished_at
		FROM agent_jobs WHERE id=?`, id).
		Scan(&job.ID, &job.DocPath, &job.CommentID, &job.AgentType, &job.Prompt,
			&job.PromptTemplateID, &caps, &repoIDs, &job.Token, &job.Status, &job.RunnerID, &lease,
			&job.Error, &created, &started, &finished)
	if err != nil {
		return AgentJob{}, false
	}
	_ = json.Unmarshal([]byte(caps), &job.Capabilities)
	_ = json.Unmarshal([]byte(repoIDs), &job.RepoIDs)
	job.LeaseUntil = parseTime(lease)
	job.CreatedAt = parseTime(created)
	job.StartedAt = parseTime(started)
	job.FinishedAt = parseTime(finished)
	return job, true
}

func (s *agentJobStore) finish(id, runnerID string, success bool, message string) error {
	now := time.Now()
	status := "error"
	if success {
		status = "done"
	}
	res, err := s.db.Exec(`UPDATE agent_jobs SET status=?,error=?,finished_at=?,lease_until=''
		WHERE id=? AND runner_id=? AND status='running'`,
		status, message, formatTime(now), id, runnerID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("任务不存在或不属于该 Runner")
	}
	return nil
}

func (s *agentJobStore) heartbeat(id, runnerID string, lease time.Duration) error {
	res, err := s.db.Exec(`UPDATE agent_jobs SET lease_until=? WHERE id=? AND runner_id=? AND status='running'`,
		formatTime(time.Now().Add(lease)), id, runnerID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("任务不存在或不属于该 Runner")
	}
	return nil
}

func (s *agentJobStore) recent(limit int) ([]AgentJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id,doc_path,comment_id,agent_type,prompt,prompt_template_id,capabilities,repo_ids,
		status,runner_id,lease_until,error,created_at,started_at,finished_at
		FROM agent_jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentJob{}
	for rows.Next() {
		var job AgentJob
		var caps, repoIDs, lease, created, started, finished string
		if err := rows.Scan(&job.ID, &job.DocPath, &job.CommentID, &job.AgentType, &job.Prompt,
			&job.PromptTemplateID, &caps, &repoIDs, &job.Status, &job.RunnerID, &lease,
			&job.Error, &created, &started, &finished); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(caps), &job.Capabilities)
		_ = json.Unmarshal([]byte(repoIDs), &job.RepoIDs)
		job.LeaseUntil = parseTime(lease)
		job.CreatedAt = parseTime(created)
		job.StartedAt = parseTime(started)
		job.FinishedAt = parseTime(finished)
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *agentJobStore) audit(jobID, token, tool, method, path string, allowed bool, reason string) {
	_, _ = s.db.Exec(`INSERT INTO agent_audit
		(id,job_id,token,tool,method,path,allowed,reason,created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		"aa_"+commentID(), jobID, token, tool, method, path, boolInt(allowed), reason, formatTime(time.Now()))
}
