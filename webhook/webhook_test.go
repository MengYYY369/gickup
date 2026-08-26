package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cooperspencer/gickup/types"
)

func TestGithubHandlerAcceptsSignedPushAndEnqueuesMatchedSource(t *testing.T) {
	body := `{"repository":{"html_url":"https://github.com/example/repo","full_name":"example/repo"}}`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	confs := []*types.Conf{{Source: types.Source{Github: []types.GenRepo{{URL: "https://github.com", User: "example"}}}}}
	queued := make(chan *types.Conf, 1)
	handler := NewGithubHandler(secret, confs, func(conf *types.Conf) bool {
		queued <- conf
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	select {
	case conf := <-queued:
		if len(conf.Source.Github) != 1 || conf.Source.Github[0].User != "example" {
			t.Fatalf("unexpected queued source: %+v", conf.Source.Github)
		}
	default:
		t.Fatal("expected matched GitHub source to be enqueued")
	}
}

func TestGithubHandlerReturnsNoContentForSignedPing(t *testing.T) {
	body := `{}`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	handler := NewGithubHandler(secret, nil, func(conf *types.Conf) bool {
		t.Fatal("ping must not enqueue")
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestGithubHandlerRejectsMissingSignature(t *testing.T) {
	body := `{"repository":{"html_url":"https://github.com/example/repo","full_name":"example/repo"}}`
	handler := NewGithubHandler("test-secret", nil, func(conf *types.Conf) bool {
		t.Fatal("missing signature must not enqueue")
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestGithubHandlerRejectsMalformedJSON(t *testing.T) {
	body := `{`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	handler := NewGithubHandler(secret, nil, func(conf *types.Conf) bool {
		t.Fatal("malformed payload must not enqueue")
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestGithubHandlerRejectsOversizedBody(t *testing.T) {
	body := strings.Repeat("x", maxBodySize+1)
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	handler := NewGithubHandler(secret, nil, func(conf *types.Conf) bool {
		t.Fatal("oversized payload must not enqueue")
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestGithubHandlerRejectsUnsupportedMethod(t *testing.T) {
	handler := NewGithubHandler("test-secret", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/webhooks/github", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestGithubHandlerReturnsServiceUnavailableWhenQueueIsFull(t *testing.T) {
	body := `{"repository":{"html_url":"https://github.com/example/repo","full_name":"example/repo"}}`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	confs := []*types.Conf{{Source: types.Source{Github: []types.GenRepo{{URL: "https://github.com", User: "example"}}}}}
	handler := NewGithubHandler(secret, confs, func(conf *types.Conf) bool { return false })

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestGithubHandlerMatchesGithubEnterpriseSource(t *testing.T) {
	body := `{"repository":{"html_url":"https://ghe.example.com/example/repo","full_name":"example/repo"}}`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	confs := []*types.Conf{{Source: types.Source{Github: []types.GenRepo{{URL: "https://GHE.EXAMPLE.COM/", User: "example"}}}}}
	queued := make(chan *types.Conf, 1)
	handler := NewGithubHandler(secret, confs, func(conf *types.Conf) bool {
		queued <- conf
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	select {
	case conf := <-queued:
		if len(conf.Source.Github) != 1 || conf.Source.Github[0].User != "example" {
			t.Fatalf("unexpected queued source: %+v", conf.Source.Github)
		}
	default:
		t.Fatal("expected enterprise GitHub source to be enqueued")
	}
}

func TestGithubHandlerRejectsAmbiguousSource(t *testing.T) {
	body := `{"repository":{"html_url":"https://github.com/example/repo","full_name":"example/repo"}}`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	confs := []*types.Conf{
		{Source: types.Source{Github: []types.GenRepo{{URL: "https://github.com", User: "example"}}}},
		{Source: types.Source{Github: []types.GenRepo{{URL: "https://github.com", Organization: "example"}}}},
	}
	handler := NewGithubHandler(secret, confs, func(conf *types.Conf) bool {
		t.Fatal("ambiguous source must not enqueue")
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestGithubHandlerMatchesSourceByRepositoryOwner(t *testing.T) {
	body := `{"repository":{"html_url":"https://github.com/example/repo","full_name":"example/repo"}}`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	confs := []*types.Conf{
		{Source: types.Source{Github: []types.GenRepo{{URL: "https://github.com", User: "example"}}}},
		{Source: types.Source{Github: []types.GenRepo{{URL: "https://github.com", User: "other"}}}},
	}
	queued := make(chan *types.Conf, 1)
	handler := NewGithubHandler(secret, confs, func(conf *types.Conf) bool {
		queued <- conf
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	conf := <-queued
	if got := conf.Source.Github[0].User; got != "example" {
		t.Fatalf("matched user = %q, want example", got)
	}
}

func TestGithubHandlerRejectsUnsupportedEvent(t *testing.T) {
	body := `{}`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	handler := NewGithubHandler(secret, nil, func(conf *types.Conf) bool {
		t.Fatal("unsupported event must not enqueue")
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", signature)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestWorkerProcessesJobsFIFOAndContinuesAfterPanic(t *testing.T) {
	var mu sync.Mutex
	var processed []string
	worker := NewWorker(3, func(conf *types.Conf) error {
		user := conf.Source.Github[0].User
		mu.Lock()
		processed = append(processed, user)
		mu.Unlock()
		if user == "panic" {
			panic("sync failed")
		}
		if user == "error" {
			return errors.New("sync failed")
		}
		return nil
	})
	worker.Start()

	for _, user := range []string{"first", "panic", "error", "last"} {
		conf := &types.Conf{Source: types.Source{Github: []types.GenRepo{{User: user}}}}
		for !worker.Enqueue(conf) {
			time.Sleep(time.Millisecond)
		}
	}
	worker.Stop()

	mu.Lock()
	defer mu.Unlock()
	want := []string{"first", "panic", "error", "last"}
	if len(processed) != len(want) {
		t.Fatalf("processed = %v, want %v", processed, want)
	}
	for i := range want {
		if processed[i] != want[i] {
			t.Fatalf("processed = %v, want %v", processed, want)
		}
	}
}

func TestWorkerRejectsEnqueueWhenQueueIsFull(t *testing.T) {
	worker := NewWorker(1, func(conf *types.Conf) error { return nil })
	first := &types.Conf{Source: types.Source{Github: []types.GenRepo{{User: "first"}}}}
	second := &types.Conf{Source: types.Source{Github: []types.GenRepo{{User: "second"}}}}

	if !worker.Enqueue(first) {
		t.Fatal("expected first enqueue to succeed")
	}
	if worker.Enqueue(second) {
		t.Fatal("expected second enqueue to fail when queue is full")
	}
}

func TestGithubHandlerRejectsInvalidSignature(t *testing.T) {
	body := `{"repository":{"html_url":"https://github.com/example/repo","full_name":"example/repo"}}`
	confs := []*types.Conf{{Source: types.Source{Github: []types.GenRepo{{URL: "https://github.com", User: "example"}}}}}
	handler := NewGithubHandler("test-secret", confs, func(conf *types.Conf) bool {
		t.Fatal("invalid signature must not enqueue")
		return true
	})

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestWorkerProcessesRepeatedPushesSeparately(t *testing.T) {
	processed := make(chan string, 2)
	worker := NewWorker(2, func(conf *types.Conf) error {
		processed <- conf.Source.Github[0].User
		return nil
	})
	worker.Start()
	conf := &types.Conf{Source: types.Source{Github: []types.GenRepo{{User: "same"}}}}
	if !worker.Enqueue(conf) || !worker.Enqueue(conf) {
		t.Fatal("expected both repeated jobs to be accepted")
	}
	worker.Stop()
	if len(processed) != 2 {
		t.Fatalf("processed %d jobs, want 2", len(processed))
	}
}

func TestNewServiceRejectsEmptySecret(t *testing.T) {
	_, err := NewService(types.WebhookConfig{Enabled: true, ListenAddr: ":0", Path: "/webhooks/github", QueueCapacity: 1}, nil, func(*types.Conf) error { return nil })
	if err == nil {
		t.Fatal("expected enabled webhook with empty secret to be rejected")
	}
}

func TestServiceHealthEndpoint(t *testing.T) {
	service, err := NewService(types.WebhookConfig{Enabled: true, ListenAddr: ":0", Path: "/webhooks/github", Secret: "secret", QueueCapacity: 1}, nil, func(*types.Conf) error { return nil })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestServiceStopWaitsForActiveRequestBeforeClosingWorker(t *testing.T) {
	syncStarted := make(chan struct{})
	releaseSync := make(chan struct{})
	service, err := NewService(types.WebhookConfig{Enabled: true, ListenAddr: ":0", Path: "/webhooks/github", Secret: "secret", QueueCapacity: 1}, nil, func(*types.Conf) error {
		close(syncStarted)
		<-releaseSync
		return nil
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.worker.Start()
	if !service.worker.Enqueue(&types.Conf{Source: types.Source{Github: []types.GenRepo{{User: "example"}}}}) {
		t.Fatal("expected synchronization job to enqueue")
	}
	<-syncStarted

	stopDone := make(chan error, 1)
	go func() { stopDone <- service.Stop() }()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop() returned before accepted work completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseSync)
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop() did not complete after accepted work finished")
	}
}

func TestGithubHandlerRejectsSignatureForDifferentBody(t *testing.T) {
	signedBody := `{"repository":{"html_url":"https://github.com/example/one"}}`
	actualBody := `{"repository":{"html_url":"https://github.com/example/two"}}`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedBody))
	handler := NewGithubHandler(secret, nil, func(*types.Conf) bool {
		t.Fatal("body-mismatched signature must not enqueue")
		return true
	})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(actualBody))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestGithubHandlerRejectsPushWithoutRepositoryIdentity(t *testing.T) {
	body := `{"repository":{}}`
	secret := "test-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	handler := NewGithubHandler(secret, []*types.Conf{{Source: types.Source{Github: []types.GenRepo{{User: "example"}}}}}, func(*types.Conf) bool {
		t.Fatal("push without repository identity must not enqueue")
		return true
	})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
