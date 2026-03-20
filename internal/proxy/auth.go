package proxy

import (
	"errors"
	"net/http"
	"strings"

	"x-tool/internal/protocol"
)

func (a *App) verifyAPIKey(r *http.Request) (string, error) {
	auth, ok := r.Header["Authorization"]
	if !ok || len(auth) == 0 {
		return "", protocol.ErrMissingAuthorization
	}

	rawAuth := strings.TrimSpace(auth[0])
	if !strings.HasPrefix(rawAuth, "Bearer ") {
		return "", protocol.ErrMissingAuthorization
	}
	clientKey := strings.TrimSpace(strings.TrimPrefix(rawAuth, "Bearer "))
	if clientKey == "" {
		return "", errors.New("empty authorization token")
	}
	if a.Config().Features.KeyPassthrough {
		return clientKey, nil
	}
	if len(a.Config().UpstreamServices) == 0 {
		return clientKey, nil
	}
	if _, ok := a.Routing().KeyToServices[clientKey]; !ok {
		return "", errors.New("unauthorized")
	}
	return clientKey, nil
}

func (a *App) verifyAnthropicAPIKey(r *http.Request) (string, error) {
	if key := strings.TrimSpace(r.Header.Get("x-api-key")); key != "" {
		if a.Config().Features.KeyPassthrough || len(a.Config().UpstreamServices) == 0 {
			return key, nil
		}
		if _, ok := a.Routing().KeyToServices[key]; ok {
			return key, nil
		}
		return "", errors.New("unauthorized")
	}

	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", protocol.ErrMissingAuthorization
	}

	key := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if key == "" {
		return "", errors.New("empty authorization token")
	}
	if a.Config().Features.KeyPassthrough || len(a.Config().UpstreamServices) == 0 {
		return key, nil
	}
	if _, ok := a.Routing().KeyToServices[key]; ok {
		return key, nil
	}
	return "", errors.New("unauthorized")
}
