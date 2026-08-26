package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	prommetrics "github.com/cooperspencer/gickup/metrics/prometheus"
	"github.com/cooperspencer/gickup/types"
	"github.com/rs/zerolog/log"
)

type syncFunc func(*types.Conf) error

type Worker struct {
	jobs chan *types.Conf
	sync syncFunc
	wg   sync.WaitGroup
}

func NewWorker(capacity int, syncSource syncFunc) *Worker {
	return &Worker{jobs: make(chan *types.Conf, capacity), sync: syncSource}
}

func (w *Worker) Enqueue(conf *types.Conf) bool {
	select {
	case w.jobs <- conf:
		prommetrics.WebhookQueueDepth.Set(float64(len(w.jobs)))
		return true
	default:
		return false
	}
}

func (w *Worker) Start() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for conf := range w.jobs {
			prommetrics.WebhookQueueDepth.Set(float64(len(w.jobs)))
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						prommetrics.WebhookSyncs.WithLabelValues("panic").Inc()
						log.Error().Interface("panic", recovered).Msg("Webhook GitHub synchronization panicked")
					}
				}()
				if w.sync != nil {
					if err := w.sync(conf); err != nil {
						prommetrics.WebhookSyncs.WithLabelValues("error").Inc()
						log.Error().Err(err).Msg("Webhook GitHub synchronization failed")
					} else {
						prommetrics.WebhookSyncs.WithLabelValues("success").Inc()
					}
				}
			}()
		}
	}()
}

type Service struct {
	server *http.Server
	worker *Worker
}

func NewService(config types.WebhookConfig, confs []*types.Conf, syncSource syncFunc) (*Service, error) {
	if config.Secret == "" {
		return nil, fmt.Errorf("webhook secret must not be empty")
	}
	if config.ListenAddr == "" {
		config.ListenAddr = ":8081"
	}
	if config.Path == "" {
		config.Path = "/webhooks/github"
	}
	if config.QueueCapacity <= 0 {
		config.QueueCapacity = 32
	}
	worker := NewWorker(config.QueueCapacity, syncSource)
	mux := http.NewServeMux()
	mux.Handle(config.Path, NewGithubHandler(config.Secret, confs, worker.Enqueue))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return &Service{server: &http.Server{Addr: config.ListenAddr, Handler: mux}, worker: worker}, nil
}

func (s *Service) Handler() http.Handler { return s.server.Handler }

func (s *Service) Start() error {
	s.worker.Start()
	log.Info().Str("listenAddr", s.server.Addr).Msg("Starting GitHub webhook listener")
	return s.server.ListenAndServe()
}

func (s *Service) Stop() error {
	err := s.server.Shutdown(context.Background())
	s.worker.Stop()
	return err
}

func (w *Worker) Stop() {
	close(w.jobs)
	w.wg.Wait()
}

const maxBodySize = 1 << 20

type enqueueFunc func(*types.Conf) bool

type githubHandler struct {
	secret  string
	confs   []*types.Conf
	enqueue enqueueFunc
}

type githubPayload struct {
	Repository struct {
		HTMLURL  string `json:"html_url"`
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func NewGithubHandler(secret string, confs []*types.Conf, enqueue enqueueFunc) http.Handler {
	return &githubHandler{secret: secret, confs: confs, enqueue: enqueue}
}

func (h *githubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		prommetrics.WebhookDeliveries.WithLabelValues("method_rejected").Inc()
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
	if err != nil {
		prommetrics.WebhookDeliveries.WithLabelValues("oversized").Inc()
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}

	if !validSignature(body, h.secret, r.Header.Get("X-Hub-Signature-256")) {
		prommetrics.WebhookDeliveries.WithLabelValues("unauthorized").Inc()
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	if event == "ping" {
		prommetrics.WebhookDeliveries.WithLabelValues("ping").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if event != "push" {
		prommetrics.WebhookDeliveries.WithLabelValues("ignored").Inc()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var payload githubPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		prommetrics.WebhookDeliveries.WithLabelValues("malformed").Inc()
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if payload.Repository.HTMLURL == "" || payload.Repository.FullName == "" {
		prommetrics.WebhookDeliveries.WithLabelValues("malformed").Inc()
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	conf := matchSource(h.confs, payload.Repository.HTMLURL, payload.Repository.FullName)
	if conf == nil {
		prommetrics.WebhookDeliveries.WithLabelValues("unmatched").Inc()
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if h.enqueue == nil || !h.enqueue(conf) {
		prommetrics.WebhookDeliveries.WithLabelValues("queue_full").Inc()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	prommetrics.WebhookDeliveries.WithLabelValues("accepted").Inc()
	log.Info().Str("repository", payload.Repository.FullName).Msg("Accepted GitHub webhook push")
	w.WriteHeader(http.StatusAccepted)
}

func validSignature(body []byte, secret, signature string) bool {
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func matchSource(confs []*types.Conf, repositoryURL, fullName string) *types.Conf {
	repoURL, err := url.Parse(repositoryURL)
	if err != nil || repoURL.Hostname() == "" {
		return nil
	}
	owner := ""
	if parts := strings.SplitN(strings.Trim(fullName, "/"), "/", 2); len(parts) == 2 {
		owner = parts[0]
	}

	var matched *types.Conf
	for _, conf := range confs {
		for _, source := range conf.Source.Github {
			sourceURL := source.URL
			if sourceURL == "" {
				sourceURL = "https://github.com"
			}
			parsed, err := url.Parse(sourceURL)
			if err != nil || !strings.EqualFold(parsed.Hostname(), repoURL.Hostname()) {
				continue
			}
			if owner != "" {
				matchesOwner := source.User != "" && strings.EqualFold(source.User, owner)
				matchesOrganization := source.Organization != "" && strings.EqualFold(source.Organization, owner)
				if !matchesOwner && !matchesOrganization {
					continue
				}
			}
			if matched != nil {
				return nil
			}
			scoped := *conf
			scoped.Source = conf.Source
			scoped.Source.Github = []types.GenRepo{source}
			matched = &scoped
		}
	}
	return matched
}
