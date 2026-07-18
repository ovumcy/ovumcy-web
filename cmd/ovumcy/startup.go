package main

import (
	"log"

	"github.com/ovumcy/ovumcy-web/internal/security"
)

func logStartup(config runtimeConfig) {
	log.Printf(
		"Ovumcy listening on http://0.0.0.0:%s (rev: %s, tz: %s, registration=%s, oidc=%t, audit_log=%t, hsts=%t, rate_limits: login=%d/%s api=%d/%s, trusted_proxy=%t)",
		config.Port,
		buildRevision(),
		config.Location.String(),
		config.RegistrationMode,
		config.OIDC.Enabled,
		config.AuditLogEnabled,
		config.HSTSEnabled,
		config.RateLimits.LoginMax,
		config.RateLimits.LoginWindow,
		config.RateLimits.APIMax,
		config.RateLimits.APIWindow,
		config.Proxy.Enabled,
	)
	if config.HSTSEnabled {
		log.Printf("NOTE: HSTS_ENABLED=true — sending Strict-Transport-Security with a 1-year max-age (max-age=31536000; includeSubDomains). Browsers will refuse plain HTTP for this host for a year; only enable when committed to HTTPS. Set HSTS_ENABLED=false to keep secure cookies without the HTTPS pin.")
	}
	if config.OIDC.Enabled && config.OIDC.ResponseMode == security.OIDCResponseModeQuery {
		log.Printf("NOTE: OIDC_RESPONSE_MODE=query — the identity provider returns the authorization code in the callback URL (a GET redirect), so it can appear in reverse-proxy and browser-history logs. This is safe: the code is unusable without the PKCE verifier, which never leaves the sealed HttpOnly state cookie. Still, restrict access to any log that records full request URLs. Set OIDC_RESPONSE_MODE=form_post (the default) if your provider supports it.")
	}
	if config.ReminderScheduler.Enabled {
		log.Printf("NOTE: REMINDER_SCHEDULER_ENABLED=true — the built-in daily reminder scheduler is ON and will make outbound webhook calls once per day at local hour %02d:00 (%s) from this process. This is an always-on component; disable it instantly with REMINDER_SCHEDULER_ENABLED=false (delivery also still requires each owner's webhook to be configured and enabled).", config.ReminderScheduler.Hour, config.Location.String())
	}
	if config.Proxy.Enabled {
		log.Printf("trusted proxy config: header=%s trusted_proxy_count=%d", config.Proxy.Header, len(config.Proxy.TrustedProxies))
	}
	if warning := proxyHeaderRateLimitWarning(config.Proxy); warning != "" {
		log.Printf("%s", warning)
	}
	if !config.CookieSecure {
		log.Printf("WARNING: COOKIE_SECURE=false — auth cookies are sent without the Secure flag and can be intercepted over plain HTTP. Set COOKIE_SECURE=true when serving over HTTPS (directly or behind a TLS-terminating proxy).")
	}
	if !config.Proxy.Enabled {
		log.Printf("WARNING: TRUST_PROXY_ENABLED=false — edge rate limiters key on the direct socket peer; behind a reverse proxy every client shares one bucket. Set TRUST_PROXY_ENABLED=true, TRUSTED_PROXIES, and a proxy-overwritten PROXY_HEADER (for example X-Real-IP) when deployed behind a proxy.")
	}
}
