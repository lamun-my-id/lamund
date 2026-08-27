package store

import (
	"testing"
)

func TestMFASetSecretAndEnable(t *testing.T) {
	st := openTemp(t)
	uid, err := st.CreateUser(User{Username: "mfauser", PasswordHash: "h", Role: "member"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Sebelum diset, secret harus kosong dan disabled
	secret, enabled, lastStep, err := st.GetMFA(uid)
	if err != nil {
		t.Fatalf("GetMFA awal: %v", err)
	}
	if secret != "" || enabled || lastStep != 0 {
		t.Fatalf("MFA awal harus kosong: secret=%q enabled=%v lastStep=%d", secret, enabled, lastStep)
	}

	// Set secret (enrollment awal, belum enabled)
	if err := st.SetMFASecret(uid, "TOTP_SECRET_BASE32"); err != nil {
		t.Fatalf("SetMFASecret: %v", err)
	}
	secret, enabled, _, err = st.GetMFA(uid)
	if err != nil {
		t.Fatalf("GetMFA setelah SetMFASecret: %v", err)
	}
	if secret != "TOTP_SECRET_BASE32" {
		t.Fatalf("secret harus tersimpan, dapat %q", secret)
	}
	if enabled {
		t.Fatal("belum diaktifkan, enabled harus false")
	}

	// Aktifkan MFA
	if err := st.EnableMFA(uid); err != nil {
		t.Fatalf("EnableMFA: %v", err)
	}
	secret, enabled, _, err = st.GetMFA(uid)
	if err != nil {
		t.Fatalf("GetMFA setelah EnableMFA: %v", err)
	}
	if secret != "TOTP_SECRET_BASE32" {
		t.Fatalf("secret berubah setelah EnableMFA: %q", secret)
	}
	if !enabled {
		t.Fatal("enabled harus true setelah EnableMFA")
	}
}

func TestMFALastStep(t *testing.T) {
	st := openTemp(t)
	uid, _ := st.CreateUser(User{Username: "mfastep", PasswordHash: "h"})

	if err := st.SetMFALastStep(uid, 5); err != nil {
		t.Fatalf("SetMFALastStep: %v", err)
	}
	_, _, lastStep, err := st.GetMFA(uid)
	if err != nil {
		t.Fatalf("GetMFA: %v", err)
	}
	if lastStep != 5 {
		t.Fatalf("lastStep harus 5, dapat %d", lastStep)
	}

	// Update ke nilai lebih besar
	if err := st.SetMFALastStep(uid, 42); err != nil {
		t.Fatalf("SetMFALastStep ke 42: %v", err)
	}
	_, _, lastStep, err = st.GetMFA(uid)
	if err != nil {
		t.Fatalf("GetMFA setelah update: %v", err)
	}
	if lastStep != 42 {
		t.Fatalf("lastStep harus 42, dapat %d", lastStep)
	}
}

func TestRecoveryCodes(t *testing.T) {
	st := openTemp(t)
	uid, _ := st.CreateUser(User{Username: "mfarecov", PasswordHash: "h"})

	hashes := []string{"ha", "hb"}
	if err := st.AddRecoveryCodes(uid, hashes); err != nil {
		t.Fatalf("AddRecoveryCodes: %v", err)
	}

	// Consume kode yang cocok dengan "ha"
	ok, err := st.ConsumeRecoveryCode(uid, func(hash string) bool { return hash == "ha" })
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode (pertama): %v", err)
	}
	if !ok {
		t.Fatal("ConsumeRecoveryCode harus true untuk hash 'ha'")
	}

	// Consume kedua kalinya dengan hash yang sama ("ha") → harus false (sudah terpakai)
	ok, err = st.ConsumeRecoveryCode(uid, func(hash string) bool { return hash == "ha" })
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode (kedua): %v", err)
	}
	if ok {
		t.Fatal("ConsumeRecoveryCode harus false karena kode sudah terpakai")
	}

	// Kode "hb" masih valid
	ok, err = st.ConsumeRecoveryCode(uid, func(hash string) bool { return hash == "hb" })
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode (hb): %v", err)
	}
	if !ok {
		t.Fatal("ConsumeRecoveryCode harus true untuk hash 'hb'")
	}

	// Hash yang tidak ada → false
	ok, err = st.ConsumeRecoveryCode(uid, func(hash string) bool { return hash == "tidak_ada" })
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode (tidak_ada): %v", err)
	}
	if ok {
		t.Fatal("ConsumeRecoveryCode harus false untuk hash yang tidak ada")
	}
}

func TestAddRecoveryCodesReplacement(t *testing.T) {
	st := openTemp(t)
	uid, _ := st.CreateUser(User{Username: "mfarepl", PasswordHash: "h"})

	// Tambahkan set pertama
	if err := st.AddRecoveryCodes(uid, []string{"lama1", "lama2"}); err != nil {
		t.Fatalf("AddRecoveryCodes pertama: %v", err)
	}

	// Tambahkan set baru — harus menggantikan yang lama
	if err := st.AddRecoveryCodes(uid, []string{"baru1", "baru2"}); err != nil {
		t.Fatalf("AddRecoveryCodes kedua: %v", err)
	}

	// Kode lama tidak boleh valid lagi
	ok, err := st.ConsumeRecoveryCode(uid, func(hash string) bool { return hash == "lama1" })
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode lama1: %v", err)
	}
	if ok {
		t.Fatal("kode lama harus tidak valid setelah AddRecoveryCodes baru")
	}

	// Kode baru harus valid
	ok, err = st.ConsumeRecoveryCode(uid, func(hash string) bool { return hash == "baru1" })
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode baru1: %v", err)
	}
	if !ok {
		t.Fatal("kode baru harus valid")
	}
}

func TestDisableMFA(t *testing.T) {
	st := openTemp(t)
	uid, _ := st.CreateUser(User{Username: "mfadisable", PasswordHash: "h"})

	// Setup MFA lengkap
	if err := st.SetMFASecret(uid, "MYSECRET"); err != nil {
		t.Fatalf("SetMFASecret: %v", err)
	}
	if err := st.EnableMFA(uid); err != nil {
		t.Fatalf("EnableMFA: %v", err)
	}
	if err := st.SetMFALastStep(uid, 100); err != nil {
		t.Fatalf("SetMFALastStep: %v", err)
	}
	if err := st.AddRecoveryCodes(uid, []string{"rc1", "rc2", "rc3"}); err != nil {
		t.Fatalf("AddRecoveryCodes: %v", err)
	}

	// Nonaktifkan MFA
	if err := st.DisableMFA(uid); err != nil {
		t.Fatalf("DisableMFA: %v", err)
	}

	// GetMFA harus mengembalikan status kosong
	secret, enabled, lastStep, err := st.GetMFA(uid)
	if err != nil {
		t.Fatalf("GetMFA setelah DisableMFA: %v", err)
	}
	if enabled {
		t.Fatal("enabled harus false setelah DisableMFA")
	}
	if secret != "" {
		t.Fatalf("secret harus kosong setelah DisableMFA, dapat %q", secret)
	}
	if lastStep != 0 {
		t.Fatalf("lastStep harus 0 setelah DisableMFA, dapat %d", lastStep)
	}

	// Recovery codes harus terhapus (consume harus false)
	ok, err := st.ConsumeRecoveryCode(uid, func(hash string) bool { return hash == "rc1" })
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode setelah DisableMFA: %v", err)
	}
	if ok {
		t.Fatal("recovery codes harus terhapus setelah DisableMFA")
	}
}
