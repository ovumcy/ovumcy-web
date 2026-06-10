package api

import (
	"fmt"
	"testing"
)

// These fixtures were sealed by the pre-consolidation codec (the revision
// where HKDF + AES-256-GCM lived inline in this package) under the fixed key
// below. They pin the complete stored format end to end: the HKDF salt/info
// labels, the derived key, the "ovumcy.cookie.<name>" AAD, the
// nonce||ciphertext layout, the v2 envelope, and the base64url transport.
//
// If any of these stops opening, key derivation or framing has changed and
// every live deployment's sealed cookies (auth, recovery, reset, OIDC state,
// TOTP, flash, ...) would be invalidated — i.e. every user logged out. Do not
// regenerate these values to make the test pass; fix the regression instead.
// A deliberate format rotation must add a new version envelope and new
// fixtures while this set keeps its documented fate (v2 open or rejected).
const goldenSealedCookieSecretKey = "golden-backcompat-secret-key-0123456789"

func TestSecureCookieCodecOpensPreConsolidationGoldenValues(t *testing.T) {
	t.Parallel()

	codec, err := newSecureCookieCodec([]byte(goldenSealedCookieSecretKey))
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}

	goldens := []struct {
		purpose string
		sealed  string
	}{
		{authCookieName, "v2.VIBSLKuwjVMy4xTlTt9kjHR4AuJew-9dygidI1ryVZlKS8MHbyPjBLvj9sWKJ6vDs__CnA"},
		{flashCookieName, "v2.2sD_NpXqvPYMPWL0wyJ8F3sDMjfwFQCvUrgHzbhkcKlXMh_-HWBxg9M5UUFyLTTO_y31WBc"},
		{recoveryCodeCookieName, "v2.zxcCF0DSlTucB1MgQEwdmQFzFseV6hQ9WBpyKausMSALoGvUogoYIeYfy9XVJJY5dSkBmBPK-NuaDZoveg"},
		{registerPickupCookieName, "v2.YMm5IAVoAs_DnTwc6I--An05FfRepbUIkEDrXtcME398JGts9tQ1p2LLqnE8fmoOvl8WRDPQBSOkxoClU_dY"},
		{resetPasswordCookieName, "v2.golsLgDUmURHcZboCdvAvoiwynu8VBuWV1CEG92EvKJ-h8KUqTNB2dRk_vJCXaXSUXsLMGLQgrhonp6XFgg"},
		{oidcStateCookieName, "v2.rmbc94eERUnhhwjU6sFQNDNLX0mOKwMNSGiI0BkyqgHho1kzflWDSkUb6Go-tT-nVNZdiEmiXTzM"},
		{oidcStepupCookieName, "v2.GXSUnh-HkP4iRXFHuNBjwKeNyv-22O9L9vfPDl_BRrI-9N41OZHOREDmABKhelECktVF_k2DTlxf4G4"},
		{oidcLogoutBridgeCookieName, "v2.DIBWpW7REF62pOimpC70E-rhzqtfM4_ckXpsZtbfhQIxXRDOgry9i87mlRmGNYCfvvHPLpII9aqOECnloWfgUBye"},
		{oidcLinkPendingCookieName, "v2.Ix1ndeGmQY91PzOhvfy8b3OBYpxm3wYu6TVawibE6yZaa05TkNyscff53E-hSU7C1HeIEjrrtBWPZWr44iBoKxw"},
		{totpPendingCookieName, "v2.M52wvyOIgi7GVk_6t-V8p9a6wQRSrbbGaG3tZJgaTBKmGWBroGh6AGlKmrDk5Ky5GvzcOR2VCxRZV2mb"},
		{totpSetupCookieName, "v2.Gm5QgnG9LpCbTluV7hz1qdwZVfABB7JeURmL8C6JQRwsYFUhRODjNOg_RYyE0pRqIe9Kqd_Vr65Jbg"},
	}

	for _, golden := range goldens {
		golden := golden
		t.Run(golden.purpose, func(t *testing.T) {
			t.Parallel()

			opened, err := codec.open(golden.purpose, golden.sealed)
			if err != nil {
				t.Fatalf("golden %q sealed by the pre-consolidation codec no longer opens: %v", golden.purpose, err)
			}
			want := fmt.Sprintf(`{"golden":%q}`, golden.purpose)
			if string(opened) != want {
				t.Fatalf("golden %q opened to %q, want %q", golden.purpose, opened, want)
			}
		})
	}
}
