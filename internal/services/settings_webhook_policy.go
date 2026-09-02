package services

// SettingsWebhookUpdatedStatus is the flash/HTMX success status emitted after a
// webhook-settings save succeeds (issue #124). It mirrors
// SettingsTrackingUpdatedStatus: a single stable outcome key that
// SettingsStatusTranslationKey maps to the localized banner copy.
const SettingsWebhookUpdatedStatus = "webhook_updated"

// SettingsWebhookRemovedStatus is the outcome key for withdrawing the stored
// endpoint. It is deliberately NOT SettingsWebhookUpdatedStatus: "saved" and
// "withdrawn" are different events to the owner, and the ledger's whole subject
// is that a surface says which one happened.
const SettingsWebhookRemovedStatus = "webhook_removed"
