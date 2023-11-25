package pkg

import (
	"errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"sigs.k8s.io/json"
	"time"
)

const webhookTimeout = 30

type WebhookServer struct {
	repositories map[string]*Repository
	deployments  map[string]*Deployment
	http.Server
}

func NewServer(cfg Config) *WebhookServer {
	// Unskippable warning if the user hasn't set up any authentication
	if cfg.SecretKey == "" && len(cfg.AllowedIPs) == 0 {
		log.Warn("Your secret_key and allowed_ips have not been configured.")
		log.Warn("This is extremely insecure, and should never be done outside of testing.")
	}

	toRet := &WebhookServer{
		repositories: make(map[string]*Repository),
		deployments:  make(map[string]*Deployment),
		Server: http.Server{
			Addr:         cfg.ListenAddr,
			WriteTimeout: (webhookTimeout + 1) * time.Second,
		},
	}

	for _, repoCfg := range cfg.Repositories {
		toRet.repositories[repoCfg.Name] = NewRepository(repoCfg)
	}
	for _, deployCfg := range cfg.Deployments {
		if deploy, err := NewDeployment(deployCfg); err != nil {
			log.WithError(err).Fatal("Invalid config")
		} else {
			toRet.deployments[deployCfg.Name] = deploy
		}
	}

	// Wrap our main HTTP handler
	handler := http.TimeoutHandler(toRet, webhookTimeout*time.Second, "Request timed out")
	if cfg.SecretKey != "" {
		handler = SecretKeyHandler(handler, "X-Key", cfg.SecretKey)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(resp http.ResponseWriter, req *http.Request) {
		resp.WriteHeader(http.StatusOK)
		_, _ = resp.Write([]byte("OK"))
	})
	mux.Handle("/", handler)

	// Allowed IPs should protect the entire mux
	if len(cfg.AllowedIPs) > 0 {
		// Parse each IP as a CIDR
		networks := ParseCIDRs(cfg.AllowedIPs)
		toRet.Server.Handler = IPAllowlistHandler(mux, networks)
	} else {
		toRet.Server.Handler = mux
	}

	return toRet
}

func (s *WebhookServer) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	logData := make(log.Fields)

	if req.Method != http.MethodPost {
		resp.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = resp.Write([]byte("Method not allowed"))
		return
	}
	// Read the payload
	payloadBytes, err := io.ReadAll(req.Body)
	if err != nil {
		log.WithError(err).Warn("Failed to read payload")
		resp.WriteHeader(500)
		_, _ = io.WriteString(resp, "Failed to read payload")
		return
	}
	// Decode the request
	var payload webhookPayload
	strictErr, err := json.UnmarshalStrict(payloadBytes, &payload, json.DisallowDuplicateFields, json.DisallowUnknownFields)
	firstError := err
	if firstError == nil && len(strictErr) > 0 {
		firstError = strictErr[0]
	}
	if firstError != nil {
		log.WithError(firstError).Warn("Failed to decode payload")
		resp.WriteHeader(500)
		_, _ = io.WriteString(resp, "Failed to decode payload")
		return
	}
	// And validate it
	if err := payload.Validate(); err != nil {
		resp.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(resp, err.Error())
		return
	}
	// Look up the deployment
	logData["deployment"] = payload.Deployment
	logData["authorized_by"] = payload.AuthorizedBy
	deployment, ok := s.deployments[payload.Deployment]
	if !ok {
		resp.WriteHeader(http.StatusNotFound)
		_, _ = resp.Write([]byte("Deployment not found"))
		return
	}
	// Look up the repository
	logData["repository"] = deployment.RepositoryName
	repo, ok := s.repositories[deployment.RepositoryName]
	if !ok {
		log.WithFields(logData).Error("Repository not found")
		resp.WriteHeader(http.StatusInternalServerError)
		_, _ = resp.Write([]byte("Internal server error"))
		return
	}
	// Lock the repository, to avoid merge conflicts
	repo.Mutex.Lock()
	defer repo.Mutex.Unlock()
	// Short circuit the repo allocations if we've already timed out
	if req.Context().Err() != nil {
		return
	}
	// Attempt to fetch the repository, with timeout
	defer repo.Discard()
	if err, details := repo.Fetch(req.Context()); err != nil {
		log.WithFields(logData).WithError(err).Warn("Failed to fetch repository")
		log.WithFields(logData).WithError(err).Debugf("Details: %s", details)
		resp.WriteHeader(http.StatusInternalServerError)
		_, _ = resp.Write([]byte("Internal server error"))
		return
	}
	// Hand the worktree to the deployment, to update
	if wt, err := repo.Worktree(); err != nil {
		log.WithFields(logData).WithError(err).Warn("Failed to fetch worktree")
		resp.WriteHeader(http.StatusInternalServerError)
		_, _ = resp.Write([]byte("Internal server error"))
		return
	} else {
		if err := deployment.Apply(wt, payload.TagName, payload.AuthorizedBy); err != nil {
			if errors.Is(err, errorNoModification) {
				resp.WriteHeader(http.StatusNotModified)
				_, _ = resp.Write([]byte("No changes made"))
				return
			}
			log.WithFields(logData).WithError(err).Warn("Failed to apply deployment")
			resp.WriteHeader(http.StatusInternalServerError)
			_, _ = resp.Write([]byte("Internal server error"))
			return
		}
	}
	// And finally, push the changes upstream
	if err, details := repo.Push(req.Context()); err != nil {
		log.WithFields(logData).WithError(err).Warn("Failed to push repository")
		log.WithFields(logData).WithError(err).Debugf("Details: %s", details)
		resp.WriteHeader(http.StatusInternalServerError)
		_, _ = resp.Write([]byte("Internal server error"))
		return
	}
	// Let the caller know we're done
	log.Infof("Deployment %s was updated to %s by %s", payload.Deployment, payload.TagName, payload.AuthorizedBy)
	resp.WriteHeader(http.StatusOK)
	_, _ = resp.Write([]byte("OK"))
}
