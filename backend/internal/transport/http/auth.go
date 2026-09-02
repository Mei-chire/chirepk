package httpapi

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName = "chirepk_session"
	sessionLifetime   = 12 * time.Hour
	feishuStateCookie = "chirepk_feishu_state"
	feishuStateMaxAge = 10 * time.Minute
)

const (
	defaultFeishuAccountsBase = "https://accounts.feishu.cn"
	defaultFeishuAPIBase      = "https://open.feishu.cn"
)

type session struct {
	username string
	expires  time.Time
}

// LocalAuthConfig enables the optional local administrator login. Both values
// must be supplied through the process environment.
type LocalAuthConfig struct {
	Username string
	Password string
}

// FeishuAuthConfig enables Feishu OAuth when all three values are present.
// AppSecret must be supplied through the process environment, never frontend code.
type FeishuAuthConfig struct {
	AppID       string
	AppSecret   string
	RedirectURL string
}

// AuthConfig contains the authentication settings owned by the HTTP layer.
type AuthConfig struct {
	Local         LocalAuthConfig
	Feishu        FeishuAuthConfig
	SecureCookies bool
}

type localAuthenticator struct {
	username string
	password string
}

type feishuAuthenticator struct {
	config       FeishuAuthConfig
	accountsBase string
	apiBase      string
	client       *http.Client
}

// Authenticator owns the short-lived browser sessions for the local admin UI.
type Authenticator struct {
	mu            sync.Mutex
	sessions      map[string]session
	local         localAuthenticator
	feishu        feishuAuthenticator
	secureCookies bool
}

func NewAuthenticator(configs ...AuthConfig) *Authenticator {
	var config AuthConfig
	if len(configs) > 0 {
		config = configs[0]
	}
	config.Local.Username = strings.TrimSpace(config.Local.Username)
	config.Local.Password = strings.TrimSpace(config.Local.Password)
	config.Feishu.AppID = strings.TrimSpace(config.Feishu.AppID)
	config.Feishu.AppSecret = strings.TrimSpace(config.Feishu.AppSecret)
	config.Feishu.RedirectURL = strings.TrimSpace(config.Feishu.RedirectURL)
	feishu := feishuAuthenticator{
		config:       config.Feishu,
		accountsBase: defaultFeishuAccountsBase,
		apiBase:      defaultFeishuAPIBase,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
	return &Authenticator{
		sessions: make(map[string]session),
		local: localAuthenticator{
			username: config.Local.Username,
			password: config.Local.Password,
		},
		feishu:        feishu,
		secureCookies: config.SecureCookies || feishu.secureCookies(),
	}
}

func (auth *Authenticator) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", auth.login)
	mux.HandleFunc("GET /api/auth/feishu/start", auth.feishuStart)
	mux.HandleFunc("GET /api/auth/feishu/callback", auth.feishuCallback)
	mux.HandleFunc("GET /api/auth/session", auth.currentSession)
	mux.HandleFunc("POST /api/auth/logout", auth.logout)
}

func (auth *Authenticator) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := auth.sessionForRequest(r); !ok {
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusUnauthorized, apiError{Error: "登录状态已失效，请重新登录"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (auth *Authenticator) login(w http.ResponseWriter, r *http.Request) {
	if !auth.local.enabled() {
		writeJSON(w, http.StatusServiceUnavailable, apiError{Error: "本地账号登录尚未配置"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &credentials); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "请输入账号和密码"})
		return
	}
	usernameOK := subtle.ConstantTimeCompare([]byte(credentials.Username), []byte(auth.local.username)) == 1
	passwordOK := subtle.ConstantTimeCompare([]byte(credentials.Password), []byte(auth.local.password)) == 1
	if !usernameOK || !passwordOK {
		writeJSON(w, http.StatusUnauthorized, apiError{Error: "账号或密码不正确"})
		return
	}

	if err := auth.startSession(w, auth.local.username); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "暂时无法登录，请稍后重试"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": auth.local.username})
}

func (auth *Authenticator) feishuStart(w http.ResponseWriter, r *http.Request) {
	if !auth.feishu.enabled() {
		http.Error(w, "飞书登录尚未配置，请先设置 FEISHU_APP_ID、FEISHU_APP_SECRET 和 FEISHU_REDIRECT_URL", http.StatusServiceUnavailable)
		return
	}
	state, err := newSessionToken()
	if err != nil {
		http.Error(w, "暂时无法开始飞书登录，请稍后重试", http.StatusInternalServerError)
		return
	}
	expires := time.Now().Add(feishuStateMaxAge)
	http.SetCookie(w, &http.Cookie{
		Name:     feishuStateCookie,
		Value:    state,
		Path:     "/api/auth/feishu/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   auth.secureCookies,
		MaxAge:   int(feishuStateMaxAge.Seconds()),
		Expires:  expires,
	})
	query := url.Values{
		"app_id":       {auth.feishu.config.AppID},
		"redirect_uri": {auth.feishu.config.RedirectURL},
		"state":        {state},
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, auth.feishu.accountsBase+"/open-apis/authen/v1/authorize?"+query.Encode(), http.StatusFound)
}

func (auth *Authenticator) feishuCallback(w http.ResponseWriter, r *http.Request) {
	if !auth.feishu.enabled() {
		http.Error(w, "飞书登录尚未配置", http.StatusServiceUnavailable)
		return
	}
	if message := strings.TrimSpace(r.URL.Query().Get("error")); message != "" {
		http.Error(w, "飞书授权未完成: "+message, http.StatusUnauthorized)
		return
	}
	stateCookie, err := r.Cookie(feishuStateCookie)
	state := r.URL.Query().Get("state")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(state)) != 1 {
		http.Error(w, "飞书登录状态无效或已过期，请重新从应用入口打开", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "飞书没有返回登录授权码", http.StatusBadRequest)
		return
	}

	accessToken, err := auth.feishu.exchangeCode(r, code)
	if err != nil {
		log.Printf("feishu token exchange failed: %v", err)
		http.Error(w, "飞书登录失败，请稍后重试", http.StatusBadGateway)
		return
	}
	username, err := auth.feishu.userName(r, accessToken)
	if err != nil {
		log.Printf("feishu user info failed: %v", err)
		http.Error(w, "获取飞书用户信息失败，请稍后重试", http.StatusBadGateway)
		return
	}
	if err := auth.startSession(w, username); err != nil {
		http.Error(w, "暂时无法建立登录会话，请稍后重试", http.StatusInternalServerError)
		return
	}
	auth.clearFeishuState(w)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (auth *Authenticator) startSession(w http.ResponseWriter, username string) error {
	token, err := newSessionToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(sessionLifetime)
	auth.mu.Lock()
	auth.pruneExpiredLocked(time.Now())
	auth.sessions[token] = session{username: username, expires: expires}
	auth.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   auth.secureCookies,
		MaxAge:   int(sessionLifetime.Seconds()),
		Expires:  expires,
	})
	return nil
}

func (auth *Authenticator) currentSession(w http.ResponseWriter, r *http.Request) {
	active, ok := auth.sessionForRequest(r)
	w.Header().Set("Cache-Control", "no-store")
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Error: "请先登录"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "username": active.username})
}

func (auth *Authenticator) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		auth.mu.Lock()
		delete(auth.sessions, cookie.Value)
		auth.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   auth.secureCookies,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (auth *Authenticator) sessionForRequest(r *http.Request) (session, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return session{}, false
	}
	now := time.Now()
	auth.mu.Lock()
	defer auth.mu.Unlock()
	active, ok := auth.sessions[cookie.Value]
	if !ok || !active.expires.After(now) {
		delete(auth.sessions, cookie.Value)
		return session{}, false
	}
	return active, true
}

func (auth *Authenticator) pruneExpiredLocked(now time.Time) {
	for token, active := range auth.sessions {
		if !active.expires.After(now) {
			delete(auth.sessions, token)
		}
	}
}

func (auth *Authenticator) clearFeishuState(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     feishuStateCookie,
		Value:    "",
		Path:     "/api/auth/feishu/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   auth.secureCookies,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func (auth feishuAuthenticator) enabled() bool {
	return auth.config.AppID != "" && auth.config.AppSecret != "" && auth.config.RedirectURL != ""
}

func (auth localAuthenticator) enabled() bool {
	return auth.username != "" && auth.password != ""
}

func (auth feishuAuthenticator) secureCookies() bool {
	redirect, err := url.Parse(auth.config.RedirectURL)
	return err == nil && redirect.Scheme == "https"
}

func (auth feishuAuthenticator) exchangeCode(r *http.Request, code string) (string, error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     auth.config.AppID,
		"client_secret": auth.config.AppSecret,
		"code":          code,
		"redirect_uri":  auth.config.RedirectURL,
	}
	var response struct {
		Code        any    `json:"code"`
		Message     string `json:"msg"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
		AccessToken string `json:"access_token"`
		Data        struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := auth.postJSON(r, "/open-apis/authen/v2/oauth/token", payload, &response); err != nil {
		return "", err
	}
	accessToken := response.AccessToken
	if accessToken == "" {
		accessToken = response.Data.AccessToken
	}
	if accessToken == "" {
		message := firstNonEmpty(response.Description, response.Message, response.Error, "飞书未返回 access_token")
		return "", errors.New(message)
	}
	return accessToken, nil
}

func (auth feishuAuthenticator) userName(r *http.Request, accessToken string) (string, error) {
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, auth.apiBase+"/open-apis/authen/v1/user_info", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := auth.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Name   string `json:"name"`
			OpenID string `json:"open_id"`
		} `json:"data"`
	}
	if err := decodeFeishuResponse(response, &result); err != nil {
		return "", err
	}
	if result.Code != 0 {
		return "", fmt.Errorf("%s (%d)", firstNonEmpty(result.Msg, "飞书接口返回错误"), result.Code)
	}
	username := firstNonEmpty(strings.TrimSpace(result.Data.Name), strings.TrimSpace(result.Data.OpenID))
	if username == "" {
		return "", errors.New("飞书未返回用户姓名或 open_id")
	}
	return username, nil
}

func (auth feishuAuthenticator) postJSON(r *http.Request, path string, payload any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodPost, auth.apiBase+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := auth.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return decodeFeishuResponse(response, target)
}

func decodeFeishuResponse(response *http.Response, target any) error {
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("飞书接口返回 HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("解析飞书接口响应失败: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func newSessionToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
