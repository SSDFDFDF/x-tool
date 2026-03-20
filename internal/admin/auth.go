package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	adminSessionCookieName = "x_tool_admin_session"
	adminSessionTTL        = 24 * time.Hour
)

type adminPasswordSource string

const (
	adminPasswordSourceNone adminPasswordSource = "none"
	adminPasswordSourceEnv  adminPasswordSource = "env"
	adminPasswordSourceDB   adminPasswordSource = "db"
)

var errAdminUnavailable = errors.New("admin password is not configured")

func (a *Admin) Available() (bool, error) {
	source, _, err := a.passwordSource()
	if err != nil {
		return false, err
	}
	return source != adminPasswordSourceNone, nil
}

func (a *Admin) RequireSession(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authenticated, err := a.authenticated(r)
		if err != nil {
			if errors.Is(err, errAdminUnavailable) {
				writeAdminUnavailable(w)
				return
			}
			writeError(w, http.StatusInternalServerError, "Failed to verify admin session", "server_error", "internal_error")
			return
		}
		if !authenticated {
			a.clearSessionCookie(w, r)
			writeAdminUnauthorized(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *Admin) passwordSource() (adminPasswordSource, string, error) {
	if a == nil || a.ConfigStore == nil {
		return adminPasswordSourceNone, "", nil
	}

	hash, err := a.ConfigStore.GetAdminPasswordHash()
	if err != nil {
		return adminPasswordSourceNone, "", err
	}
	if strings.TrimSpace(hash) != "" {
		return adminPasswordSourceDB, hash, nil
	}
	if strings.TrimSpace(a.EnvAdminPassword) != "" {
		return adminPasswordSourceEnv, a.EnvAdminPassword, nil
	}
	return adminPasswordSourceNone, "", nil
}

func (a *Admin) verifyPassword(password string) (adminPasswordSource, bool, error) {
	source, credential, err := a.passwordSource()
	if err != nil {
		return adminPasswordSourceNone, false, err
	}

	switch source {
	case adminPasswordSourceDB:
		err := bcrypt.CompareHashAndPassword([]byte(credential), []byte(password))
		if err == nil {
			return source, true, nil
		}
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return source, false, nil
		}
		return source, false, err
	case adminPasswordSourceEnv:
		return source, secureEqual(password, credential), nil
	default:
		return adminPasswordSourceNone, false, nil
	}
}

func (a *Admin) authenticated(r *http.Request) (bool, error) {
	source, _, err := a.passwordSource()
	if err != nil {
		return false, err
	}
	if source == adminPasswordSourceNone {
		return false, errAdminUnavailable
	}
	if a == nil || a.ConfigStore == nil {
		return false, errAdminUnavailable
	}

	token, ok := a.sessionToken(r)
	if !ok {
		return false, nil
	}

	now := time.Now().UTC()
	if err := a.ConfigStore.DeleteExpiredAdminSessions(now); err != nil {
		return false, err
	}

	valid, err := a.ConfigStore.TouchAdminSession(hashAdminSessionToken(token), now)
	if err != nil {
		return false, err
	}
	return valid, nil
}

func (a *Admin) issueSession(w http.ResponseWriter, r *http.Request) error {
	if a == nil || a.ConfigStore == nil {
		return errAdminUnavailable
	}

	now := time.Now().UTC()
	if err := a.ConfigStore.DeleteExpiredAdminSessions(now); err != nil {
		return err
	}

	token, err := newAdminSessionToken()
	if err != nil {
		return err
	}
	expiresAt := now.Add(adminSessionTTL)
	if err := a.ConfigStore.CreateAdminSession(hashAdminSessionToken(token), expiresAt, now); err != nil {
		return err
	}

	http.SetCookie(w, buildSessionCookie(r, token, expiresAt))
	return nil
}

func (a *Admin) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, expiredSessionCookie(r))
}

func (a *Admin) sessionToken(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil {
		return "", false
	}
	token := strings.TrimSpace(cookie.Value)
	if token == "" {
		return "", false
	}
	return token, true
}

func buildSessionCookie(r *http.Request, token string, expiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	return &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   maxAge,
	}
}

func expiredSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	}
}

func isSecureRequest(r *http.Request) bool {
	if r != nil && r.TLS != nil {
		return true
	}
	if r == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func writeAdminUnauthorized(w http.ResponseWriter) {
	writeError(w, http.StatusUnauthorized, "Unauthorized", "authentication_error", "unauthorized")
}

func writeAdminUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "Admin is unavailable: password is not configured", "server_error", "admin_unavailable")
}

func newAdminSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashAdminSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func secureEqual(actual, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}
