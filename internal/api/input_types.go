package api

type credentialsInput struct {
	Email           string `json:"email" form:"email"`
	Password        string `json:"password" form:"password"`
	ConfirmPassword string `json:"confirm_password" form:"confirm_password"`
	RememberMe      bool   `json:"remember_me" form:"remember_me"`
}

type dayPayload struct {
	IsPeriod      bool    `json:"is_period"`
	Flow          string  `json:"flow"`
	Mood          int     `json:"mood"`
	SexActivity   string  `json:"sex_activity"`
	BBT           float64 `json:"bbt"`
	CervicalMucus string  `json:"cervical_mucus"`
	SymptomIDs    []uint  `json:"symptom_ids"`
	Notes         string  `json:"notes"`
}

type symptomPayload struct {
	Name  string `json:"name" form:"name"`
	Icon  string `json:"icon" form:"icon"`
	Color string `json:"color" form:"color"`
}

type forgotPasswordInput struct {
	Email        string `json:"email" form:"email"`
	RecoveryCode string `json:"recovery_code" form:"recovery_code"`
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

type cycleSettingsInput struct {
	CycleLength        int    `json:"cycle_length" form:"cycle_length"`
	PeriodLength       int    `json:"period_length" form:"period_length"`
	AutoPeriodFill     bool   `json:"auto_period_fill" form:"auto_period_fill"`
	IrregularCycle     bool   `json:"irregular_cycle" form:"irregular_cycle"`
	LastPeriodStart    string `json:"last_period_start" form:"last_period_start"`
	LastPeriodStartSet bool   `json:"-" form:"-"`
}

type profileSettingsInput struct {
	DisplayName string `json:"display_name" form:"display_name"`
}

type trackingSettingsInput struct {
	TrackBBT           bool   `json:"track_bbt" form:"track_bbt"`
	TemperatureUnit    string `json:"temperature_unit" form:"temperature_unit"`
	TrackCervicalMucus bool   `json:"track_cervical_mucus" form:"track_cervical_mucus"`
	HideSexChip        bool   `json:"hide_sex_chip" form:"hide_sex_chip"`
}

type deleteAccountInput struct {
	Password string `json:"password" form:"password"`
}
