package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAuthenticatorLoginProtectAndLogout(t *testing.T) {
	auth := NewAuthenticator(AuthConfig{Local: LocalAuthConfig{
		Username: "test-admin",
		Password: "test-password",
	}})
	protected := auth.Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	unauthorized := httptest.NewRecorder()
	protected.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	invalid := httptest.NewRecorder()
	auth.login(invalid, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"test-admin","password":"wrong"}`)))
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid login status = %d, want %d", invalid.Code, http.StatusUnauthorized)
	}

	login := httptest.NewRecorder()
	auth.login(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"test-admin","password":"test-password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d", login.Code, http.StatusOK)
	}
	response := login.Result()
	cookies := response.Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly {
		t.Fatalf("login cookie = %#v, want one HttpOnly %q cookie", cookies, sessionCookieName)
	}
	cookie := cookies[0]

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	authorizedRequest.AddCookie(cookie)
	authorized := httptest.NewRecorder()
	protected.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authenticated status = %d, want %d", authorized.Code, http.StatusNoContent)
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	sessionRequest.AddCookie(cookie)
	sessionRecorder := httptest.NewRecorder()
	auth.currentSession(sessionRecorder, sessionRequest)
	if sessionRecorder.Code != http.StatusOK {
		t.Fatalf("session status = %d, want %d", sessionRecorder.Code, http.StatusOK)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutRequest.AddCookie(cookie)
	logoutRecorder := httptest.NewRecorder()
	auth.logout(logoutRecorder, logoutRequest)
	if logoutRecorder.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", logoutRecorder.Code, http.StatusNoContent)
	}

	afterLogoutRequest := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	afterLogoutRequest.AddCookie(cookie)
	afterLogout := httptest.NewRecorder()
	protected.ServeHTTP(afterLogout, afterLogoutRequest)
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout status = %d, want %d", afterLogout.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticatorLeavesHealthPublic(t *testing.T) {
	auth := NewAuthenticator()
	handler := auth.Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestFeishuLoginRequiresConfiguration(t *testing.T) {
	auth := NewAuthenticator()
	recorder := httptest.NewRecorder()
	auth.feishuStart(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/feishu/start", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("start status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestLocalLoginRequiresConfiguration(t *testing.T) {
	auth := NewAuthenticator()
	recorder := httptest.NewRecorder()
	auth.login(recorder, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"test-admin","password":"test-password"}`)))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("login status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestFeishuOAuthFlowCreatesSession(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/authen/v2/oauth/token":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode token request: %v", err)
			}
			if body["client_id"] != "cli_test" || body["client_secret"] != "test-secret" || body["code"] != "login-code" {
				t.Fatalf("unexpected token request: %#v", body)
			}
			if body["redirect_uri"] != "https://schedule.example.test/api/auth/feishu/callback" {
				t.Fatalf("redirect_uri = %q", body["redirect_uri"])
			}
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "access_token": "user-token"})
		case "/open-apis/authen/v1/user_info":
			if r.Header.Get("Authorization") != "Bearer user-token" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]string{"name": "测试用户", "open_id": "ou_test"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	auth := NewAuthenticator(AuthConfig{Feishu: FeishuAuthConfig{
		AppID:       "cli_test",
		AppSecret:   "test-secret",
		RedirectURL: "https://schedule.example.test/api/auth/feishu/callback",
	}})
	auth.feishu.accountsBase = provider.URL
	auth.feishu.apiBase = provider.URL
	auth.feishu.client = provider.Client()

	startRecorder := httptest.NewRecorder()
	auth.feishuStart(startRecorder, httptest.NewRequest(http.MethodGet, "/api/auth/feishu/start", nil))
	if startRecorder.Code != http.StatusFound {
		t.Fatalf("start status = %d, want %d", startRecorder.Code, http.StatusFound)
	}
	location, err := url.Parse(startRecorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse authorization location: %v", err)
	}
	if location.Path != "/open-apis/authen/v1/authorize" || location.Query().Get("app_id") != "cli_test" {
		t.Fatalf("authorization location = %q", location.String())
	}
	state := location.Query().Get("state")
	var stateCookie *http.Cookie
	for _, cookie := range startRecorder.Result().Cookies() {
		if cookie.Name == feishuStateCookie {
			stateCookie = cookie
		}
	}
	if state == "" || stateCookie == nil || stateCookie.Value != state {
		t.Fatalf("state cookie does not match authorization state")
	}

	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/auth/feishu/callback?code=login-code&state="+url.QueryEscape(state), nil)
	callbackRequest.AddCookie(stateCookie)
	callbackRecorder := httptest.NewRecorder()
	auth.feishuCallback(callbackRecorder, callbackRequest)
	if callbackRecorder.Code != http.StatusSeeOther || callbackRecorder.Header().Get("Location") != "/" {
		t.Fatalf("callback response = %d %q", callbackRecorder.Code, callbackRecorder.Header().Get("Location"))
	}
	var sessionCookie *http.Cookie
	for _, cookie := range callbackRecorder.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("callback did not create a session cookie")
	}
	if !sessionCookie.Secure {
		t.Fatal("HTTPS OAuth callback must create a Secure session cookie")
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	sessionRequest.AddCookie(sessionCookie)
	sessionRecorder := httptest.NewRecorder()
	auth.currentSession(sessionRecorder, sessionRequest)
	if sessionRecorder.Code != http.StatusOK || !strings.Contains(sessionRecorder.Body.String(), "测试用户") {
		t.Fatalf("session response = %d %s", sessionRecorder.Code, sessionRecorder.Body.String())
	}
}
