package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gptbox-server/internal/session"
)

func TestPublicICloudHMEMailRejectsMissingCredentialsWithoutSession(t *testing.T) {
	handler := NewRouter(nil, session.NewManager([]byte("test-secret"), false))
	for _, path := range []string{
		"/api/public/icloud-hme/mail/latest",
		"/api/public/icloud-hme/mail/history",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", path, recorder.Code)
		}
		for _, header := range []string{"Cache-Control", "Pragma", "Referrer-Policy"} {
			if recorder.Header().Get(header) == "" {
				t.Fatalf("%s missing %s", path, header)
			}
		}
	}
}

func TestRequestLoggingDoesNotIncludePublicMailQuery(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	handler := NewRouter(nil, session.NewManager([]byte("test-secret"), false))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/public/icloud-hme/mail/latest?address=alias%40icloud.com&key=secret-key", nil)
	handler.ServeHTTP(recorder, request)
	value := output.String()
	if bytes.Contains(output.Bytes(), []byte("secret-key")) || bytes.Contains(output.Bytes(), []byte("alias%40icloud.com")) {
		t.Fatalf("request log leaked query: %s", value)
	}
	if !bytes.Contains(output.Bytes(), []byte("path=/api/public/icloud-hme/mail/latest")) {
		t.Fatalf("request log missing safe path: %s", value)
	}
}

func TestPublicICloudHMEMailRejectsUnsupportedMethod(t *testing.T) {
	handler := NewRouter(nil, session.NewManager([]byte("test-secret"), false))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/public/icloud-hme/mail/latest", nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}

func TestICloudHMEPublicLimiterEnforcesLimits(t *testing.T) {
	limiter := newICloudHMEPublicLimiter()
	now := time.Now()
	for index := 0; index < publicMailFailureLimit; index++ {
		limiter.recordAuthenticationFailure("ip-alias", now)
	}
	if allowed, retry := limiter.allowAuthentication("ip-alias", now); allowed || retry <= 0 {
		t.Fatalf("allowed = %v retry = %v", allowed, retry)
	}
	if allowed, _ := limiter.allowAuthentication("ip-alias", now.Add(publicMailFailureWindow)); !allowed {
		t.Fatal("failure window did not reset")
	}
	for index := 0; index < publicMailAliasLimit; index++ {
		if allowed, _ := limiter.allowAlias("alias", now); !allowed {
			t.Fatalf("alias request %d unexpectedly denied", index)
		}
	}
	if allowed, retry := limiter.allowAlias("alias", now); allowed || retry <= 0 {
		t.Fatalf("alias limit allowed = %v retry = %v", allowed, retry)
	}
}
