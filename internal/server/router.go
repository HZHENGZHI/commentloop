package server

import (
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
)

const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: https: http:; font-src 'self' data:; " +
	"connect-src 'self' ws: wss:; object-src 'none'; base-uri 'self'; " +
	"form-action 'none'; frame-ancestors 'none'"

func init() {
	gin.SetMode(gin.ReleaseMode)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.engine.ServeHTTP(w, r)
}

func (s *Server) routes() *gin.Engine {
	r := gin.New()
	r.RedirectTrailingSlash = false
	r.RedirectFixedPath = false
	r.HandleMethodNotAllowed = true
	r.Use(requestLogger, gin.CustomRecoveryWithWriter(io.Discard, recoverPanic), s.secureHeaders)

	api := r.Group("/api", s.agentTokenGuard)
	api.GET("/health", s.api(s.handleHealth))
	api.GET("/config", s.api(s.handleConfig))
	api.GET("/tree", s.api(s.handleTree))
	api.GET("/doc", s.api(s.handleGetDoc))
	api.PUT("/doc", s.api(s.requireSameOrigin(s.handleSaveDoc)))
	api.POST("/doc", s.api(s.requireSameOrigin(s.handleSaveDoc)))
	api.DELETE("/doc", s.api(s.requireSameOrigin(s.handleDeleteDoc)))
	api.GET("/highlight.css", s.api(s.handleHighlightCSS))
	api.GET("/browse", s.api(s.requireSameOrigin(s.handleBrowse)))
	api.POST("/render", s.api(s.requireSameOrigin(s.handleRender)))
	api.POST("/roots", s.api(s.requireSameOrigin(s.handleAddRoot)))
	api.POST("/roots/remote", s.api(s.requireSameOrigin(s.handleAddRemoteRoot)))
	api.POST("/agent/refresh", s.api(s.requireSameOrigin(s.handleAgentRefresh)))
	api.GET("/inbox", s.api(s.handleInbox))
	api.GET("/doc/review", s.api(s.handleGetDocReview))
	api.POST("/doc/review", s.api(s.requireSameOrigin(s.handleSetDocReview)))
	api.GET("/comments", s.api(s.handleGetComments))
	api.POST("/comments", s.api(s.requireSameOrigin(s.handleCreateComment)))
	api.POST("/comments/reply", s.api(s.requireSameOrigin(s.handleReplyComment)))
	api.POST("/comments/resolve", s.api(s.requireSameOrigin(s.handleResolveComment)))
	api.POST("/comments/group", s.api(s.requireSameOrigin(s.handleGroupComment)))
	api.POST("/comments/rollback", s.api(s.requireSameOrigin(s.handleRollbackComment)))
	api.POST("/comments/trigger", s.api(s.requireSameOrigin(s.handleTriggerCommentAgent)))
	api.GET("/code/repos", s.api(s.handleListCodeRepos))
	api.DELETE("/code/repos", s.api(s.requireSameOrigin(s.handleDeleteCodeRepo)))
	api.GET("/doc/code-repos", s.api(s.handleDocCodeRepos))
	api.POST("/doc/code-repos", s.api(s.requireSameOrigin(s.handleAddDocCodeRepo)))
	api.DELETE("/doc/code-repos", s.api(s.requireSameOrigin(s.handleRemoveDocCodeRepo)))
	api.GET("/code/info", s.api(s.guardAgentRepo(s.handleCodeRepoInfo)))
	api.GET("/code/files", s.api(s.guardAgentRepo(s.handleCodeRepoFiles)))
	api.GET("/code/file", s.api(s.guardAgentRepo(s.handleCodeRepoFile)))
	api.GET("/code/search", s.api(s.guardAgentRepo(s.handleCodeRepoSearch)))
	api.GET("/code/branches", s.api(s.guardAgentRepo(s.handleCodeRepoBranches)))
	api.POST("/code/branch", s.api(s.requireSameOrigin(s.handleSwitchCodeRepoBranch)))
	api.POST("/lsp/definition", s.api(s.requireSameOrigin(s.handleLSPDefinition)))
	api.GET("/settings", s.api(s.handleGetSettings))
	api.GET("/settings/defaults", s.api(s.handleGetDefaultSettings))
	api.PUT("/settings", s.api(s.requireSameOrigin(s.handleSaveSettings)))
	api.POST("/settings/check-command", s.api(s.requireSameOrigin(s.handleCheckCommand)))
	api.POST("/agent/runner/claim", s.api(s.handleAgentRunnerClaim))
	api.POST("/agent/runner/finish", s.api(s.handleAgentRunnerFinish))
	api.POST("/agent/runner/heartbeat", s.api(s.handleAgentRunnerHeartbeat))
	api.GET("/agent/jobs", s.api(s.handleListAgentJobs))
	api.GET("/prompt-templates", s.api(s.handleListPromptTemplates))
	api.POST("/prompt-templates", s.api(s.requireSameOrigin(s.handleCreatePromptTemplate)))
	api.PUT("/prompt-templates", s.api(s.requireSameOrigin(s.handleUpdatePromptTemplate)))
	api.DELETE("/prompt-templates", s.api(s.requireSameOrigin(s.handleDeletePromptTemplate)))
	api.POST("/prompt-templates/reset", s.api(s.requireSameOrigin(s.handleResetPromptTemplate)))
	api.POST("/prompt-templates/optimize", s.api(s.requireSameOrigin(s.handleOptimizePromptTemplate)))
	api.GET("/doc/discussions", s.api(s.handleDocDiscussions))
	api.GET("/doc/discussions/available", s.api(s.handleAvailableDiscussionSessions))
	api.POST("/doc/discussions", s.api(s.requireSameOrigin(s.handleImportDiscussion)))
	api.DELETE("/doc/discussions", s.api(s.requireSameOrigin(s.handleDeleteDiscussion)))
	api.POST("/doc/consensus/refresh", s.api(s.requireSameOrigin(s.handleRefreshConsensus)))
	api.GET("/doc/agent-sessions", s.api(s.handleGetAgentSessions))
	api.POST("/doc/agent-sessions", s.api(s.requireSameOrigin(s.handleCreateAgentSession)))
	api.POST("/doc/agent-sessions/active", s.api(s.requireSameOrigin(s.handleSetActiveAgentSession)))
	api.PATCH("/doc/agent-sessions", s.api(s.requireSameOrigin(s.handleRenameAgentSession)))
	api.DELETE("/comments", s.api(s.requireSameOrigin(s.handleDeleteComment)))
	api.POST("/remote/session", s.api(s.requireSameOrigin(s.handleCreateRemoteSession)))
	api.GET("/remote/browse", s.api(s.requireSameOrigin(s.handleBrowseRemote)))
	api.DELETE("/remote/session", s.api(s.requireSameOrigin(s.handleCloseRemoteSession)))
	api.DELETE("/roots", s.api(s.requireSameOrigin(s.handleRemoveRoot)))

	r.Any("/ws", gin.WrapF(s.handleWS))
	r.Any("/raw/*path", gin.WrapF(s.handleRaw))
	r.NoMethod(s.noMethod)
	r.NoRoute(s.noRoute)
	return r
}

// api adapts a handler to the API conventions: English error localization
// and agent tool auditing for successful requests.
func (s *Server) api(h http.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		r := c.Request
		aw := &apiErrorWriter{ResponseWriter: c.Writer, status: http.StatusOK, lang: promptLanguage(r)}
		h(aw, r)
		aw.finish()
		if aw.status < 400 {
			s.auditAgentRequest(r)
		}
	}
}

func (s *Server) auditAgentRequest(r *http.Request) {
	grant, known := s.agentGrantForRequest(r)
	if !known {
		return
	}
	token := r.Header.Get("X-Commentloop-Agent-Token")
	tool := r.Header.Get("X-Commentloop-MCP-Tool")
	if tool == "" {
		tool = inferAgentTool(r.Method, r.URL.Path)
	}
	s.agentJobs.audit(grant.JobID, token, tool, r.Method, r.URL.Path, true, "")
	if tool != "" && grant.DocPath != "" {
		s.broadcastAgent(grant.DocPath, "working", agentToolProgress(tool))
	}
}

func (s *Server) secureHeaders(c *gin.Context) {
	h := c.Writer.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	if isAPIPath(c.Request.URL.Path) {
		h.Set("Cache-Control", "no-store")
	}
	if !s.hostAllowed(c.Request) {
		if isAPIPath(c.Request.URL.Path) {
			writeLocalizedError(c.Writer, c.Request, http.StatusForbidden, errForbiddenHost)
		} else {
			http.Error(c.Writer, "forbidden host", http.StatusForbidden)
		}
		c.Abort()
		return
	}
	c.Next()
}

func (s *Server) agentTokenGuard(c *gin.Context) {
	if s.rejectInvalidAgentToken(c) {
		c.Abort()
		return
	}
	c.Next()
}

func (s *Server) rejectInvalidAgentToken(c *gin.Context) bool {
	r := c.Request
	if !s.agentTokenInvalid(r) || strings.HasPrefix(r.URL.Path, "/api/agent/runner/") {
		return false
	}
	message := errAgentJobTokenInvalidExpired.ZH
	if promptLanguage(r) == "en" {
		message = errAgentJobTokenInvalidExpired.EN
	}
	s.recordAgentDenial(r, message)
	writeLocalizedError(c.Writer, r, http.StatusForbidden, errAgentJobTokenInvalidExpired)
	return true
}

func (s *Server) noMethod(c *gin.Context) {
	if isAPIPath(c.Request.URL.Path) && s.rejectInvalidAgentToken(c) {
		return
	}
	if !isAPIPath(c.Request.URL.Path) {
		http.Error(c.Writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	writeLocalizedError(c.Writer, c.Request, http.StatusMethodNotAllowed, errMethodNotAllowed)
}

func (s *Server) noRoute(c *gin.Context) {
	// Gin presets 404 for NoRoute handlers; reset it so handlers that write a
	// body without an explicit WriteHeader still respond 200.
	c.Status(http.StatusOK)
	if !isAPIPath(c.Request.URL.Path) {
		s.handleStatic(c.Writer, c.Request)
		return
	}
	if s.rejectInvalidAgentToken(c) {
		return
	}
	s.api(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, errAPIEndpointNotFound)
	})(c)
}

func isAPIPath(p string) bool {
	return strings.HasPrefix(p, "/api/")
}

func recoverPanic(c *gin.Context, err any) {
	slog.Error("panic serving request", "method", c.Request.Method, "path", c.Request.URL.Path,
		"err", err, "stack", string(debug.Stack()))
	if isAPIPath(c.Request.URL.Path) && !c.Writer.Written() {
		writeLocalizedError(c.Writer, c.Request, http.StatusInternalServerError, errInternalServer)
		c.Abort()
		return
	}
	c.AbortWithStatus(http.StatusInternalServerError)
}
