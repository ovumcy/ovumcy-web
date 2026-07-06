package services

// SettingsWebhookUpdatedStatus is the flash/HTMX success status emitted after a
// webhook-settings save succeeds (issue #124). It mirrors
// SettingsTrackingUpdatedStatus: a single stable outcome key that
// SettingsStatusTranslationKey maps to the localized banner copy.
const SettingsWebhookUpdatedStatus = "webhook_updated"
