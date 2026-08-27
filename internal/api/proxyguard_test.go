package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
)

// TestTenantCannotCustomProxy: tenant (member) tak boleh reverse-proxy ke upstream
// custom — baik lewat site type=proxy maupun route type=proxy. Operator boleh.
func TestTenantCannotCustomProxy(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := auth.HashPassword("rahasia123")
	st.CreateUser(store.User{Username: "admin", PasswordHash: hash, Role: "superadmin"})
	secret, _ := auth.GenerateSecret()
	h := New(Deps{Store: st, Issuer: auth.NewIssuer(secret, time.Hour), Sites: store.NewSiteFS(t.TempDir())})
	tok := mkUser(t, h, st, "tenant1", 5)

	// 1. member bikin site proxy custom → 403
	rec := do(t, h, "POST", "/api/v1/sites", tok, map[string]any{
		"type": "proxy", "domain": "p.example.com", "proxy_target": "https://backend.example.com",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member site proxy harus 403, dapat %d: %s", rec.Code, rec.Body)
	}

	// 2. superadmin bikin site proxy custom → 201 (operator escape hatch)
	atok := adminToken(t, h)
	rec = do(t, h, "POST", "/api/v1/sites", atok, map[string]any{
		"type": "proxy", "domain": "ops.example.com", "proxy_target": "https://backend.example.com",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("superadmin site proxy harus 201, dapat %d: %s", rec.Code, rec.Body)
	}

	// 3. member: bikin site static, lalu route proxy custom → 403
	do(t, h, "POST", "/api/v1/sites", tok, map[string]any{"type": "static", "domain": "s.example.com"})
	rrec := do(t, h, "PUT", "/api/v1/sites/s.example.com/routes", tok, map[string]any{
		"routes": []map[string]any{{"path_prefix": "/api", "type": "proxy", "upstream": "https://backend.example.com"}},
	})
	if rrec.Code != http.StatusForbidden {
		t.Fatalf("member route proxy harus 403, dapat %d: %s", rrec.Code, rrec.Body)
	}
}
