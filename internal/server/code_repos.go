package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hzhengzhi/commentloop/internal/coderepo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func newCodeRepoStore(db *gorm.DB, stateDir string) *codeRepoStore {
	cacheRoot := ""
	if strings.TrimSpace(stateDir) != "" {
		cacheRoot = filepath.Join(stateDir, "code-repos")
	} else {
		cacheRoot = filepath.Join(os.TempDir(), "commentloop-code-repos")
	}
	return &codeRepoStore{db: db, cacheRoot: cacheRoot}
}

func (r codeRepoRow) record() codeRepoRecord {
	return codeRepoRecord{
		ID: r.ID, Name: r.Name, Path: r.Path, Kind: r.Kind,
		RemoteURL: r.RemoteURL, RemoteBranch: r.RemoteBranch, CreatedAt: parseTime(r.CreatedAt),
	}
}

func codeReposFromRows(rows []codeRepoRow, linked bool) []codeRepoRecord {
	out := make([]codeRepoRecord, 0, len(rows))
	for _, row := range rows {
		item := row.record()
		item.Linked = linked
		out = append(out, item)
	}
	return out
}

func (s *codeRepoStore) list() ([]codeRepoRecord, error) {
	var rows []codeRepoRow
	if err := s.db.Order("name, path").Find(&rows).Error; err != nil {
		return nil, err
	}
	return codeReposFromRows(rows, false), nil
}

func (s *codeRepoStore) find(query string, args ...any) (codeRepoRecord, bool) {
	var row codeRepoRow
	if err := s.db.Where(query, args...).Take(&row).Error; err != nil {
		return codeRepoRecord{}, false
	}
	return row.record(), true
}

func (s *codeRepoStore) get(id string) (codeRepoRecord, bool) {
	return s.find("id = ?", id)
}

func (s *codeRepoStore) ensure(path string) (codeRepoRecord, error) {
	normalized, name, err := coderepo.Normalize(path)
	if err != nil {
		return codeRepoRecord{}, err
	}
	if existing, ok := s.byPath(normalized); ok {
		return existing, nil
	}
	id := "repo_" + commentID()
	now := time.Now()
	if err := s.db.Create(&codeRepoRow{
		ID: id, Name: name, Path: normalized, Kind: "local", CreatedAt: formatTime(now),
	}).Error; err != nil {
		// A concurrent add may have won the unique path race.
		if existing, ok := s.byPath(normalized); ok {
			return existing, nil
		}
		return codeRepoRecord{}, err
	}
	return codeRepoRecord{ID: id, Name: name, Path: normalized, Kind: "local", CreatedAt: now}, nil
}

func (s *codeRepoStore) byPath(path string) (codeRepoRecord, bool) {
	return s.find("path = ?", path)
}

func (s *codeRepoStore) link(docPath, repoID string) error {
	if _, ok := s.get(repoID); !ok {
		return os.ErrNotExist
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&docCodeRepoRow{
		DocPath: docPath, RepoID: repoID, CreatedAt: formatTime(time.Now()),
	}).Error
}

func (s *codeRepoStore) unlink(docPath, repoID string) error {
	result := s.db.Where("doc_path = ? AND repo_id = ?", docPath, repoID).Delete(&docCodeRepoRow{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return os.ErrNotExist
	}
	return nil
}

func (s *codeRepoStore) linked(docPath string) ([]codeRepoRecord, error) {
	var rows []codeRepoRow
	if err := s.db.Joins("JOIN doc_code_repos d ON d.repo_id = code_repos.id").
		Where("d.doc_path = ?", docPath).
		Order("code_repos.name, code_repos.path").Find(&rows).Error; err != nil {
		return nil, err
	}
	return codeReposFromRows(rows, true), nil
}

func (s *codeRepoStore) delete(id string) error {
	result := s.db.Where("id = ?", id).Delete(&codeRepoRow{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return os.ErrNotExist
	}
	return nil
}

func (s *codeRepoStore) enriched(item codeRepoRecord, linked bool) codeRepoRecord {
	info := coderepo.GitInfo(item.Path)
	item.Branch = info.Branch
	item.Head = info.Head
	item.Dirty = info.Dirty
	item.Changed = info.Changed
	item.Linked = linked
	return item
}

func (s *Server) codeRepoForID(id string) (codeRepoRecord, bool) {
	return s.codeRepos.get(id)
}

func (s *Server) handleListCodeRepos(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentCodeRead(w, r) {
		return
	}
	items, err := s.codeRepos.list()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if grant, known := s.agentGrantForRequest(r); known {
		allowed := make(map[string]bool, len(grant.RepoIDs))
		for _, id := range grant.RepoIDs {
			allowed[id] = true
		}
		filtered := items[:0]
		for _, item := range items {
			if allowed[item.ID] {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	for i := range items {
		items[i] = s.codeRepos.enriched(items[i], false)
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": items})
}

func (s *Server) handleDocCodeRepos(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentCodeRead(w, r) {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, errMissingDocumentPath)
		return
	}
	ro, rel, err := s.resolveDocRef(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if s.rejectAgentDocPath(w, r, canonical, "当前任务只能读取关联文档的代码仓库") {
		return
	}
	items, err := s.codeRepos.linked(canonical)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for i := range items {
		items[i] = s.codeRepos.enriched(items[i], true)
	}
	if items == nil {
		items = []codeRepoRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": canonical, "repos": items})
}

func (s *Server) handleAddDocCodeRepo(w http.ResponseWriter, r *http.Request) {
	var req docCodeRepoRequest
	if !bindJSON(w, r, 32*1024, &req) {
		return
	}
	ro, rel, err := s.resolveDocRef(req.DocPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	var item codeRepoRecord
	if req.RepoID != "" {
		item, _ = s.codeRepos.get(req.RepoID)
		if item.ID == "" {
			writeError(w, http.StatusNotFound, errCodeRepositoryNotFound)
			return
		}
	} else if strings.TrimSpace(req.RemoteURL) != "" {
		ctx, cancel := context.WithTimeout(r.Context(), coderepo.GitHubCloneTimeout)
		defer cancel()
		item, err = s.codeRepos.ensureGitHub(ctx, req.RemoteURL, req.RemoteBranch)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	} else {
		item, err = s.codeRepos.ensure(req.RepoPath)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := s.codeRepos.link(canonical, item.ID); err != nil {
		writeErrorDetail(w, http.StatusInternalServerError, errFailedLinkCodeRepository, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.codeRepos.enriched(item, true))
}

func (s *Server) handleRemoveDocCodeRepo(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	id := r.URL.Query().Get("id")
	ro, rel, err := s.resolveDocRef(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, errInvalidPathNotMarkdownFile)
		return
	}
	canonical := ro.ID + "/" + rel
	if err := s.codeRepos.unlink(canonical, id); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errLinkNotFound)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteCodeRepo(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if err := s.codeRepos.delete(id); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errCodeRepositoryNotFound)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCodeRepoInfo(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentCodeRead(w, r) {
		return
	}
	item, ok := s.codeRepoForID(r.URL.Query().Get("repo"))
	if !ok {
		writeError(w, http.StatusNotFound, errCodeRepositoryNotFound)
		return
	}
	info := coderepo.GitInfo(item.Path)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": item.ID, "name": item.Name, "path": item.Path,
		"branch": info.Branch, "head": info.Head, "dirty": info.Dirty, "changed": info.Changed,
	})
}

func (s *Server) handleCodeRepoFiles(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentCodeRead(w, r) {
		return
	}
	item, ok := s.codeRepoForID(r.URL.Query().Get("repo"))
	if !ok {
		writeError(w, http.StatusNotFound, errCodeRepositoryNotFound)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := coderepo.List(item.Path, r.URL.Query().Get("query"), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCodeRepoFile(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentCodeRead(w, r) {
		return
	}
	item, ok := s.codeRepoForID(r.URL.Query().Get("repo"))
	if !ok {
		writeError(w, http.StatusNotFound, errCodeRepositoryNotFound)
		return
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	end, _ := strconv.Atoi(r.URL.Query().Get("end"))
	result, err := coderepo.Read(item.Path, r.URL.Query().Get("path"), start, end)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCodeRepoSearch(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentCodeRead(w, r) {
		return
	}
	item, ok := s.codeRepoForID(r.URL.Query().Get("repo"))
	if !ok {
		writeError(w, http.StatusNotFound, errCodeRepositoryNotFound)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := coderepo.Search(item.Path, r.URL.Query().Get("q"), limit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *codeRepoStore) byRemote(remoteURL, branch string) (codeRepoRecord, bool) {
	return s.find("remote_url = ? AND remote_branch = ?", remoteURL, branch)
}

func (s *codeRepoStore) ensureGitHub(ctx context.Context, remoteURL, branch string) (codeRepoRecord, error) {
	source, err := coderepo.ParseGitHub(remoteURL)
	if err != nil {
		return codeRepoRecord{}, err
	}
	branch = strings.TrimSpace(branch)
	if existing, ok := s.byRemote(source.CloneURL, branch); ok {
		if _, err := os.Stat(filepath.Join(existing.Path, ".git")); err == nil {
			return existing, nil
		}
		token := githubToken()
		if err := coderepo.CloneGitHub(ctx, source, branch, existing.Path, token); err != nil {
			return codeRepoRecord{}, err
		}
		return existing, nil
	}
	id := "repo_" + commentID()
	dest := filepath.Join(s.cacheRoot, id)
	token := githubToken()
	if err := coderepo.CloneGitHub(ctx, source, branch, dest, token); err != nil {
		return codeRepoRecord{}, err
	}
	now := time.Now()
	item := codeRepoRecord{
		ID: id, Name: source.Name, Path: dest, Kind: "github",
		RemoteURL: source.CloneURL, RemoteBranch: branch, CreatedAt: now,
	}
	if err := s.db.Create(&codeRepoRow{
		ID: item.ID, Name: item.Name, Path: item.Path, Kind: item.Kind,
		RemoteURL: item.RemoteURL, RemoteBranch: item.RemoteBranch, CreatedAt: formatTime(now),
	}).Error; err != nil {
		_ = os.RemoveAll(dest)
		return codeRepoRecord{}, err
	}
	return item, nil
}

func githubToken() string {
	for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (s *codeRepoStore) switchGitHubBranch(ctx context.Context, id, branch string) (codeRepoRecord, error) {
	item, ok := s.get(id)
	if !ok {
		return codeRepoRecord{}, os.ErrNotExist
	}
	if item.Kind != "github" {
		return codeRepoRecord{}, fmt.Errorf("只有 GitHub 仓库支持切换分支")
	}
	source, err := coderepo.ParseGitHub(item.RemoteURL)
	if err != nil {
		return codeRepoRecord{}, err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return codeRepoRecord{}, fmt.Errorf("分支名不能为空")
	}
	if err := coderepo.UpdateGitHubBranch(ctx, source, item.Path, branch, githubToken()); err != nil {
		return codeRepoRecord{}, err
	}
	if err := s.db.Model(&codeRepoRow{}).Where("id = ?", id).Update("remote_branch", branch).Error; err != nil {
		return codeRepoRecord{}, err
	}
	item.RemoteBranch = branch
	return item, nil
}

func (s *codeRepoStore) githubBranches(ctx context.Context, id string) ([]string, error) {
	item, ok := s.get(id)
	if !ok {
		return nil, os.ErrNotExist
	}
	if item.Kind != "github" {
		return nil, fmt.Errorf("只有 GitHub 仓库支持分支列表")
	}
	source, err := coderepo.ParseGitHub(item.RemoteURL)
	if err != nil {
		return nil, err
	}
	return coderepo.ListGitHubBranches(ctx, source, item.Path, githubToken())
}

func (s *codeRepoStore) branches(ctx context.Context, id string) ([]string, error) {
	item, ok := s.get(id)
	if !ok {
		return nil, os.ErrNotExist
	}
	if item.Kind == "github" {
		return s.githubBranches(ctx, id)
	}
	return coderepo.LocalBranches(ctx, item.Path)
}

func (s *codeRepoStore) switchBranch(ctx context.Context, id, branch string) (codeRepoRecord, error) {
	item, ok := s.get(id)
	if !ok {
		return codeRepoRecord{}, os.ErrNotExist
	}
	if item.Kind == "github" {
		return s.switchGitHubBranch(ctx, id, branch)
	}
	if err := coderepo.SwitchLocalBranch(ctx, item.Path, branch); err != nil {
		return codeRepoRecord{}, err
	}
	return item, nil
}

func (s *Server) handleCodeRepoBranches(w http.ResponseWriter, r *http.Request) {
	if s.rejectAgentCodeRead(w, r) {
		return
	}
	item, ok := s.codeRepos.get(r.URL.Query().Get("id"))
	if !ok {
		writeError(w, http.StatusNotFound, errCodeRepositoryNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	branches, err := s.codeRepos.branches(ctx, item.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": branches})
}

func (s *Server) handleSwitchCodeRepoBranch(w http.ResponseWriter, r *http.Request) {
	var req switchBranchRequest
	if !bindJSON(w, r, 16*1024, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if _, err := s.codeRepos.switchBranch(ctx, req.ID, req.Branch); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, errCodeRepositoryNotFound)
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.lsp.Reset()
	item, _ := s.codeRepos.get(req.ID)
	writeJSON(w, http.StatusOK, s.codeRepos.enriched(item, false))
}
