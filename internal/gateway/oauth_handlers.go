package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/audit"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/session"
)

// OAuthHandlers holds dependencies for OIDC SSO endpoints (spec ④).
type OAuthHandlers struct {
	oauthManager *security.OAuthManager
	cookieAuth   *security.CookieAuth
	auth         *security.Authenticator
	store        session.UserWorkspaceStore
	log          *slog.Logger
	now          func() time.Time
}

// NewOAuthHandlers constructs OAuth SSO handlers. auth is the Authenticator
// whose audit collector receives the auth.login row at the SSO credential-
// exchange boundary (mirrors the password Login handler).
func NewOAuthHandlers(oauthManager *security.OAuthManager, cookieAuth *security.CookieAuth, auth *security.Authenticator, store session.UserWorkspaceStore, log *slog.Logger) *OAuthHandlers {
	if log == nil {
		log = slog.Default()
	}
	return &OAuthHandlers{
		oauthManager: oauthManager,
		cookieAuth:   cookieAuth,
		auth:         auth,
		store:        store,
		log:          log.With("component", "oauth_handler"),
		now:          time.Now,
	}
}

// Providers: GET /api/auth/oauth/providers
// Lists configured SSO providers for the login page to render buttons.
func (h *OAuthHandlers) Providers(w http.ResponseWriter, r *http.Request) {
	providers := h.oauthManager.List()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"providers": providers,
	})
}

// Login: GET /api/auth/oauth/{provider}/login
// Initiates the OIDC authorization code flow. Generates state + PKCE verifier,
// stores them in a signed short-lived cookie, and redirects to the IdP.
func (h *OAuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")
	if providerName == "" {
		writeAppError(w, http.StatusBadRequest, "BAD_REQUEST", "provider required")
		return
	}

	provider, ok := h.oauthManager.Lookup(providerName)
	if !ok {
		writeAppError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider not configured")
		return
	}

	state, codeVerifier, codeChallenge, err := security.GenerateStateAndVerifier()
	if err != nil {
		h.log.Error("generate state failed", "provider", providerName, "err", err)
		writeAppError(w, http.StatusInternalServerError, "INTERNAL", "state generation failed")
		return
	}

	// Store state + PKCE verifier in signed cookie (5min TTL).
	security.SetStateCookie(w, r, h.cookieAuth, security.StateCookiePayload{
		State:        state,
		CodeVerifier: codeVerifier,
		Provider:     providerName,
		IssuedAt:     h.now(),
	})

	callbackURL := h.callbackURL(r, providerName)
	authURL := provider.BuildAuthURL(security.AuthURLOption{
		State:         state,
		CodeChallenge: codeChallenge,
		RedirectURL:   callbackURL,
	})

	h.log.Debug("oauth login redirect", "provider", providerName, "state", state[:8]+"...")
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback: GET /api/auth/oauth/{provider}/callback
// Handles the IdP redirect after user authentication. Validates state (CSRF),
// exchanges code for tokens, verifies ID Token, finds or creates the user,
// and issues a session cookie.
func (h *OAuthHandlers) Callback(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")
	if providerName == "" {
		redirectAuthError(w, r, "BAD_REQUEST")
		return
	}

	provider, ok := h.oauthManager.Lookup(providerName)
	if !ok {
		redirectAuthError(w, r, "PROVIDER_NOT_FOUND")
		return
	}

	// Check for IdP error response.
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		h.log.Warn("oauth callback: idp error", "provider", providerName, "err", errCode)
		redirectAuthError(w, r, "IDP_ERROR")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		redirectAuthError(w, r, "BAD_REQUEST")
		return
	}

	// Verify state cookie (CSRF + PKCE verifier + provider binding).
	payload, err := security.VerifyStateCookie(r, h.cookieAuth, state, providerName)
	if err != nil {
		h.log.Warn("oauth callback: state verification failed", "provider", providerName, "err", err)
		redirectAuthError(w, r, classifyStateError(err))
		return
	}

	// Exchange authorization code for tokens (with PKCE verifier).
	callbackURL := h.callbackURL(r, providerName)
	exchangeResult, err := provider.ExchangeCodeWithRedirectURL(r.Context(), code, payload.CodeVerifier, callbackURL)
	if err != nil {
		h.log.Error("oauth callback: token exchange failed", "provider", providerName, "err", err)
		redirectAuthError(w, r, "CODE_EXCHANGE_FAILED")
		return
	}

	// Verify ID Token signature and extract claims.
	claims, err := provider.VerifyAndExtractClaimsWithUserInfo(r.Context(), exchangeResult.IDToken, exchangeResult.AccessToken)
	if err != nil {
		h.log.Error("oauth callback: id_token verification failed", "provider", providerName, "err", err)
		redirectAuthError(w, r, "ID_TOKEN_INVALID")
		return
	}

	// Find or create user from SSO identity.
	userID, firstLogin, err := h.getOrCreateUser(r.Context(), providerName, claims)
	if err != nil {
		var idErr *security.IdentityError
		if errors.As(err, &idErr) && idErr.Code == security.ErrCodeUserDisabled {
			h.log.Warn("oauth callback: user disabled", "provider", providerName, "subject", claims.Subject)
			// Identity resolved but rejected (disabled) — credential-boundary failure.
			if h.auth != nil {
				h.auth.EmitAuthEvent(audit.ActionAuthLogin, audit.OutcomeFailure, audit.AnonymousUserID,
					audit.PlatformWebChat, audit.UserIDTypeAnonymous, security.ExtractClientIP(r), r.UserAgent(), r.URL.Path, r.Method)
			}
			redirectAuthError(w, r, "USER_DISABLED")
		} else {
			h.log.Error("oauth callback: user creation failed", "provider", providerName, "subject", claims.Subject, "err", err)
			if h.auth != nil {
				h.auth.EmitAuthEvent(audit.ActionAuthLogin, audit.OutcomeFailure, audit.AnonymousUserID,
					audit.PlatformWebChat, audit.UserIDTypeAnonymous, security.ExtractClientIP(r), r.UserAgent(), r.URL.Path, r.Method)
			}
			redirectAuthError(w, r, "USER_CREATE_FAILED")
		}
		return
	}

	// Issue session cookie (same as password login).
	if err := h.cookieAuth.SetCookie(w, r, userID); err != nil {
		h.log.Error("oauth callback: cookie issuance failed", "provider", providerName, "user_id", userID, "err", err)
		redirectAuthError(w, r, "INTERNAL")
		return
	}

	// Authoritative SSO login row: a registered user completed credential
	// exchange via an IdP (spec §5.4: webchat → PlatformWebChat + registered).
	if h.auth != nil {
		h.auth.EmitAuthEvent(audit.ActionAuthLogin, audit.OutcomeSuccess, userID,
			audit.PlatformWebChat, audit.UserIDTypeRegistered, security.ExtractClientIP(r), r.UserAgent(), r.URL.Path, r.Method)
	}

	// Clear the OAuth state cookie (one-time use).
	security.ClearStateCookie(w, r)

	// Touch last login.
	_ = h.store.TouchUserLastLogin(r.Context(), userID, h.now().Unix())

	h.log.Info("oauth login success", "provider", providerName, "subject", claims.Subject, "user_id", userID)

	// Redirect to webchat home. Echo first_login for first-time SSO users so the
	// frontend triggers onboarding — mirroring password login's first_login flag
	// (all three login paths now share one onboarding contract). Determined by
	// getOrCreateUser from LastLoginAt BEFORE this handler's TouchUserLastLogin.
	dest := "/"
	if firstLogin {
		dest = "/?first_login=1"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// getOrCreateUser implements the spec ④ §8.1 account association in one store
// transaction: resolve by (provider, subject), otherwise create/reuse the
// deterministic SSO user and bind the identity.
func (h *OAuthHandlers) getOrCreateUser(ctx context.Context, providerName string, claims *security.OIDCClaims) (userID string, firstLogin bool, err error) {
	now := h.now().Unix()
	username := providerName + ":" + claims.Subject

	result, err := h.store.GetOrCreateUserByIdentity(ctx, providerName, claims.Subject, username, claims.DisplayName, claims.Email, uuid.NewString(), uuid.NewString(), now)
	if err != nil {
		return "", false, err
	}
	if result.User.Status == "disabled" {
		return "", false, &security.IdentityError{Code: security.ErrCodeUserDisabled}
	}
	return result.User.ID, result.User.LastLoginAt == 0, nil
}

func (h *OAuthHandlers) callbackURL(r *http.Request, providerName string) string {
	if base := strings.TrimRight(h.oauthManager.ExternalURL(), "/"); base != "" {
		return base + "/api/auth/oauth/" + providerName + "/callback"
	}
	return requestPublicBaseURL(r) + "/api/auth/oauth/" + providerName + "/callback"
}

func requestPublicBaseURL(r *http.Request) string {
	scheme, host := forwardedProtoHost(r)
	if scheme == "" {
		if r.TLS != nil || strings.EqualFold(firstHeaderValue(r.Header.Get("X-Forwarded-Proto")), "https") {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	if host == "" {
		host = firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
	}
	if host == "" {
		host = r.Host
	}
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}

func forwardedProtoHost(r *http.Request) (scheme, host string) {
	for _, part := range strings.Split(r.Header.Get("Forwarded"), ",") {
		for _, item := range strings.Split(part, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(item), "=")
			if !ok {
				continue
			}
			v = strings.Trim(v, `"`)
			switch strings.ToLower(k) {
			case "proto":
				if scheme == "" {
					scheme = v
				}
			case "host":
				if host == "" {
					host = v
				}
			}
		}
		if scheme != "" || host != "" {
			break
		}
	}
	if scheme == "" {
		scheme = firstHeaderValue(r.Header.Get("X-Forwarded-Proto"))
	}
	if host == "" {
		host = firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
	}
	if scheme != "" && !strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https") {
		scheme = ""
	}
	return scheme, host
}

func firstHeaderValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// redirectAuthError redirects to webchat home with an auth_error query param.
// spec ⑥ frontend will render an error message from this param.
func redirectAuthError(w http.ResponseWriter, r *http.Request, code string) {
	security.ClearStateCookie(w, r)
	http.Redirect(w, r, "/?auth_error="+code, http.StatusFound)
}

// classifyStateError maps state verification errors to error codes.
func classifyStateError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "expired"):
		return "STATE_EXPIRED"
	case strings.Contains(msg, "provider mismatch"):
		return "PROVIDER_MISMATCH"
	case strings.Contains(msg, "csrf"):
		return "CSRF_DETECTED"
	case strings.Contains(msg, "cookie missing"):
		return "CSRF_DETECTED"
	default:
		return "STATE_INVALID"
	}
}
