package services

import (
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const defaultOIDCLogoutStateTTL = 7 * 24 * time.Hour

type OIDCLogoutStateStore interface {
	Save(state *models.OIDCLogoutState) error
	FindBySessionID(sessionID string) (models.OIDCLogoutState, bool, error)
	DeleteBySessionID(sessionID string) error
	DeleteExpired(cutoff time.Time) error
}

type OIDCLogoutStateService struct {
	store OIDCLogoutStateStore
}

func NewOIDCLogoutStateService(store OIDCLogoutStateStore) *OIDCLogoutStateService {
	return &OIDCLogoutStateService{store: store}
}

func (service *OIDCLogoutStateService) Save(sessionID string, state OIDCLogoutState, now time.Time) error {
	if service == nil || service.store == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	expiresAt := now.Add(defaultOIDCLogoutStateTTL)

	if err := service.store.DeleteExpired(now); err != nil {
		return err
	}
	return service.store.Save(&models.OIDCLogoutState{
		SessionID:             sessionID,
		EndSessionEndpoint:    strings.TrimSpace(state.EndSessionEndpoint),
		IDTokenHint:           strings.TrimSpace(state.IDTokenHint),
		PostLogoutRedirectURL: strings.TrimSpace(state.PostLogoutRedirectURL),
		ExpiresAt:             expiresAt,
		CreatedAt:             now,
		UpdatedAt:             now,
	})
}

func (service *OIDCLogoutStateService) Load(sessionID string, now time.Time) (OIDCLogoutState, bool, error) {
	return service.load(sessionID, now, false)
}

func (service *OIDCLogoutStateService) Consume(sessionID string, now time.Time) (OIDCLogoutState, bool, error) {
	return service.load(sessionID, now, true)
}

func (service *OIDCLogoutStateService) Delete(sessionID string) error {
	if service == nil || service.store == nil {
		return nil
	}
	return service.store.DeleteBySessionID(strings.TrimSpace(sessionID))
}

func (service *OIDCLogoutStateService) load(sessionID string, now time.Time, consume bool) (OIDCLogoutState, bool, error) {
	if service == nil || service.store == nil {
		return OIDCLogoutState{}, false, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return OIDCLogoutState{}, false, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	if err := service.store.DeleteExpired(now); err != nil {
		return OIDCLogoutState{}, false, err
	}

	record, found, err := service.store.FindBySessionID(sessionID)
	if err != nil || !found {
		return OIDCLogoutState{}, false, err
	}
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now) {
		if deleteErr := service.store.DeleteBySessionID(sessionID); deleteErr != nil {
			return OIDCLogoutState{}, false, deleteErr
		}
		return OIDCLogoutState{}, false, nil
	}
	if consume {
		if err := service.store.DeleteBySessionID(sessionID); err != nil {
			return OIDCLogoutState{}, false, err
		}
	}

	return OIDCLogoutState{
		EndSessionEndpoint:    strings.TrimSpace(record.EndSessionEndpoint),
		IDTokenHint:           strings.TrimSpace(record.IDTokenHint),
		PostLogoutRedirectURL: strings.TrimSpace(record.PostLogoutRedirectURL),
	}, true, nil
}
