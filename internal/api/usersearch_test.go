package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

// TestRankUserMatches: exact selalu di atas, typo ≤2 terjaring, urutan benar.
func TestRankUserMatches(t *testing.T) {
	cands := []store.UserLite{
		{Username: "wahyudin", Name: "Wahyu"},
		{Username: "wahyudi", Name: "Wahyudi X"},   // prefix
		{Username: "mywahyudinx", Name: ""},         // substring
		{Username: "wahyodin", Name: ""},            // typo distance 1
		{Username: "zzzz", Name: "wahyudin team"},   // name substring
		{Username: "unrelated", Name: "nothing"},    // dibuang
	}
	got := rankUserMatches(cands, "wahyudin", 8)
	if len(got) < 4 {
		t.Fatalf("harusnya >=4 match, dapat %d: %+v", len(got), got)
	}
	if got[0].Username != "wahyudin" {
		t.Fatalf("exact match harus di atas, dapat %q", got[0].Username)
	}
	// 'unrelated' tak boleh muncul.
	for _, u := range got {
		if u.Username == "unrelated" {
			t.Fatal("kandidat tak relevan bocor ke hasil")
		}
	}
	// wahyodin (typo d=1) harus ada.
	found := false
	for _, u := range got {
		if u.Username == "wahyodin" {
			found = true
		}
	}
	if !found {
		t.Fatal("typo 1 huruf (wahyodin) harus terjaring")
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		d    int
	}{
		{"wahyudin", "wahyudin", 0},
		{"wahyudin", "wahyodin", 1},
		{"wahyudin", "wahodin", 2},
		{"abc", "xyz", 3},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.d {
			t.Errorf("levenshtein(%q,%q)=%d, mau %d", c.a, c.b, got, c.d)
		}
	}
}

// TestSearchUsersEndpoint: min 4 huruf; exact di atas; approved-only.
func TestSearchUsersEndpoint(t *testing.T) {
	h, st := harness(t)
	hash, _ := auth.HashPassword("rahasia123")
	st.CreateUser(store.User{Username: "wahyudin", PasswordHash: hash, Role: "member", Name: "Wahyu"})
	st.CreateUser(store.User{Username: "wahyudi", PasswordHash: hash, Role: "member"})
	// pending user tak boleh muncul.
	st.CreateUserPending(store.User{Username: "wahyupending", PasswordHash: hash, Role: "member"})
	tok := adminToken(t, h)

	// <4 huruf → kosong.
	rec := do(t, h, "GET", "/api/v1/users/search?q=wah", tok, nil)
	var short struct{ Users []store.UserLite }
	json.Unmarshal(rec.Body.Bytes(), &short)
	if len(short.Users) != 0 {
		t.Fatalf("query <4 harus kosong, dapat %d", len(short.Users))
	}

	// >=4 huruf → hasil, exact di atas, pending tak muncul.
	rec = do(t, h, "GET", "/api/v1/users/search?q=wahyudin", tok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("harus 200, dapat %d", rec.Code)
	}
	var res struct{ Users []store.UserLite }
	json.Unmarshal(rec.Body.Bytes(), &res)
	if len(res.Users) == 0 || res.Users[0].Username != "wahyudin" {
		t.Fatalf("exact 'wahyudin' harus di atas, dapat %+v", res.Users)
	}
	for _, u := range res.Users {
		if u.Username == "wahyupending" {
			t.Fatal("user pending tak boleh muncul di pencarian")
		}
	}
}
