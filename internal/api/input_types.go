package api

type credentialsInput struct {
	Email           string `json:"email" form:"email"`
	Password        string `json:"password" form:"password"`
	ConfirmPassword string `json:"confirm_password" form:"confirm_password"`
	RememberMe      bool   `json:"remember_me" form:"remember_me"`
	Consent         string `json:"consent" form:"consent"`
}

// dayPayload is the transport shape of a day save. ConfirmCycleStart carries
// the answer to the inline cycle-start question the HTML day form asks beside
// the period toggle; it is read from the form body only and stays out of the
// published v1 JSON body (`json:"-"`), which keeps marking a cycle start over
// JSON the dedicated POST /api/v1/days/:date/cycle-start endpoint's job.
type dayPayload struct {
	IsPeriod          bool `json:"is_period"`
	ConfirmCycleStart bool `json:"-"`

	Flow            string   `json:"flow"`
	Mood            int      `json:"mood"`
	SexActivity     string   `json:"sex_activity"`
	BBT             *float64 `json:"bbt"`
	CervicalMucus   string   `json:"cervical_mucus"`
	PregnancyTest   string   `json:"pregnancy_test"`
	CycleFactorKeys []string `json:"cycle_factor_keys"`
	SymptomIDs      []uint   `json:"symptom_ids"`
	Notes           string   `json:"notes"`
}

type symptomPayload struct {
	Name  string `json:"name" form:"name"`
	Icon  string `json:"icon" form:"icon"`
	Color string `json:"color" form:"color"`
}

type totpChallengeInput struct {
	Code string `json:"code" form:"code"`
}

// forgotPasswordInput carries both step-1 (email only) and step-2 (email +
// recovery code + the account's CURRENT password) submissions of
// POST /api/v1/password-resets. Password is the account's existing password,
// never a new one: the recovery code substitutes for the second factor, not for
// the first (docs/SECURITY_INVARIANTS.md → Password recovery).
type forgotPasswordInput struct {
	Email        string `json:"email" form:"email"`
	RecoveryCode string `json:"recovery_code" form:"recovery_code"`
	Password     string `json:"password" form:"password"`
}

type resetPasswordInput struct {
	Password        string `json:"password" form:"password"`
	ConfirmPassword string `json:"confirm_password" form:"confirm_password"`
}

type changePasswordInput struct {
	CurrentPassword string `json:"current_password" form:"current_password"`
	NewPassword     string `json:"new_password" form:"new_password"`
	ConfirmPassword string `json:"confirm_password" form:"confirm_password"`
}

type profileSettingsInput struct {
	DisplayName string `json:"display_name" form:"display_name"`
}

type interfaceSettingsInput struct {
	Language string `json:"language" form:"language"`
	Theme    string `json:"theme" form:"theme"`
}

// trackingSettingsInput is the published v1 JSON body of
// PATCH /api/v1/users/current/tracking. The three section toggles keep the
// stored, inverted spelling here because renaming a v1 field is a breaking
// change (CONTRIBUTING "API Stability Contract"); trackingUpdate converts them
// into the positive view model through the services conversion point, so the
// transport layer never performs the negation itself. The bundled HTML form
// posts the positive show_* fields instead — see parseTrackingSettingsInput.
type trackingSettingsInput struct {
	TrackBBT             bool   `json:"track_bbt" form:"track_bbt"`
	TemperatureUnit      string `json:"temperature_unit" form:"temperature_unit"`
	TrackCervicalMucus   bool   `json:"track_cervical_mucus" form:"track_cervical_mucus"`
	HideSexChip          bool   `json:"hide_sex_chip" form:"hide_sex_chip"`
	HideCycleFactors     bool   `json:"hide_cycle_factors" form:"hide_cycle_factors"`
	HideNotesField       bool   `json:"hide_notes_field" form:"hide_notes_field"`
	ShowHistoricalPhases bool   `json:"show_historical_phases" form:"show_historical_phases"`
	WeekStartsOn         string `json:"week_starts_on" form:"week_starts_on"`
}

// webhookSettingsInput is the transport shape of the webhook-settings form
// (issue #124). URL is WRITE-ONLY: the settings page never renders the stored
// endpoint, so a blank URL means "leave the stored endpoint unchanged". The
// handler forwards these fields to WebhookSettingsService, which owns the
// keep/replace/remove semantics, validation, and encryption — the URL secret is
// never decrypted, re-encrypted, or logged in the transport layer.
type webhookSettingsInput struct {
	Enabled         bool   `json:"webhook_enabled" form:"webhook_enabled"`
	URL             string `json:"webhook_url" form:"webhook_url"`
	NotifyPeriod    bool   `json:"webhook_notify_period" form:"webhook_notify_period"`
	NotifyOvulation bool   `json:"webhook_notify_ovulation" form:"webhook_notify_ovulation"`
	RemoveURL       bool   `json:"webhook_remove_url" form:"webhook_remove_url"`
}

type passwordProtectedSettingsInput struct {
	Password string `json:"password" form:"password"`
}

type timezoneSettingsInput struct {
	Timezone string `json:"timezone" form:"timezone"`
}

type reminderSettingsInput struct {
	ReminderLeadDays int `json:"reminder_lead_days" form:"reminder_lead_days"`
}
