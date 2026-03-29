package models

import "time"

type OIDCLogoutState struct {
	SessionID             string    `gorm:"column:session_id;primaryKey"`
	EndSessionEndpoint    string    `gorm:"column:end_session_endpoint;not null"`
	IDTokenHint           string    `gorm:"column:id_token_hint;not null"`
	PostLogoutRedirectURL string    `gorm:"column:post_logout_redirect_url;not null"`
	ExpiresAt             time.Time `gorm:"column:expires_at;not null"`
	CreatedAt             time.Time `gorm:"column:created_at;not null"`
	UpdatedAt             time.Time `gorm:"column:updated_at;not null"`
}

func (OIDCLogoutState) TableName() string {
	return "oidc_logout_states"
}
