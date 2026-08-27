package api

import (
	"strings"
	"testing"
)

func TestEmailSettingsKeepSecretOnBlank(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	do(t, h, "PUT", "/api/v1/email/settings", tok, map[string]any{"backend": "smtp", "host": "m1", "port": 587, "from": "a@x", "password": "secret1", "tls": true})
	// Simpan ulang dengan password kosong tapi host berubah — password harus dipertahankan.
	do(t, h, "PUT", "/api/v1/email/settings", tok, map[string]any{"backend": "smtp", "host": "m2", "port": 587, "from": "a@x", "password": "", "tls": true})
	got, err := st.GetEmailSettings()
	if err != nil {
		t.Fatalf("GetEmailSettings gagal: %v", err)
	}
	if got.Host != "m2" {
		t.Fatalf("host harus terupdate ke m2, dapat %q", got.Host)
	}
	if got.Password != "secret1" {
		t.Fatalf("password blank harus mempertahankan secret1, dapat %q", got.Password)
	}
}

func TestEmailSettingsKeepAPIKeyOnBlank(t *testing.T) {
	h, st := harness(t)
	tok := loginToken(t, h)
	do(t, h, "PUT", "/api/v1/email/settings", tok, map[string]any{"backend": "mailersend", "host": "", "port": 0, "from": "a@x", "api_key": "key1", "tls": false})
	// Simpan ulang dengan api_key kosong — api_key harus dipertahankan.
	do(t, h, "PUT", "/api/v1/email/settings", tok, map[string]any{"backend": "mailersend", "host": "", "port": 0, "from": "b@x", "api_key": "", "tls": false})
	got, err := st.GetEmailSettings()
	if err != nil {
		t.Fatalf("GetEmailSettings gagal: %v", err)
	}
	if got.From != "b@x" {
		t.Fatalf("from harus terupdate ke b@x, dapat %q", got.From)
	}
	if got.APIKey != "key1" {
		t.Fatalf("api_key blank harus mempertahankan key1, dapat %q", got.APIKey)
	}
}

func TestEmailSettingsSuperadminOnly(t *testing.T) {
	h, st := harness(t) // admin=superadmin
	_, tokMem := newMember(t, h, st, "plain")
	if rec := do(t, h, "GET", "/api/v1/email/settings", tokMem, nil); rec.Code != 403 {
		t.Fatalf("member akses setelan email harus 403, dapat %d", rec.Code)
	}
	tok := loginToken(t, h)
	put := map[string]any{"backend": "smtp", "host": "mail.x", "port": 587, "from": "a@x", "password": "secret", "tls": true}
	if rec := do(t, h, "PUT", "/api/v1/email/settings", tok, put); rec.Code != 200 {
		t.Fatalf("superadmin PUT harus 200, dapat %d: %s", rec.Code, rec.Body)
	}
	rec := do(t, h, "GET", "/api/v1/email/settings", tok, nil)
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatal("password TIDAK boleh bocor di GET")
	}
}
