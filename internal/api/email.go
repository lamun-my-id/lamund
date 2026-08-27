package api

import (
	"net/http"

	"github.com/lamun-my-id/lamund/internal/store"
)

func (s *server) registerEmail(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/email/settings", s.requireAdmin(s.handleGetEmailSettings))
	mux.HandleFunc("PUT /api/v1/email/settings", s.requireAdmin(s.handlePutEmailSettings))
	mux.HandleFunc("POST /api/v1/email/test", s.requireAdmin(s.handleTestEmail))
}

func (s *server) handleGetEmailSettings(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.d.Store.GetEmailSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal membaca setelan email")
		return
	}
	// Mask rahasia — tidak pernah dikembalikan ke klien.
	cfg.Password = ""
	cfg.APIKey = ""
	writeJSON(w, http.StatusOK, cfg)
}

type emailSettingsReq struct {
	Backend  string `json:"backend"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	TLS      bool   `json:"tls"`
	APIBase  string `json:"api_base"`
	APIKey   string `json:"api_key"`
}

func (s *server) handlePutEmailSettings(w http.ResponseWriter, r *http.Request) {
	var req emailSettingsReq
	if err := decode(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	cfg := store.EmailSettings{
		Backend:  req.Backend,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
		From:     req.From,
		TLS:      req.TLS,
		APIBase:  req.APIBase,
		APIKey:   req.APIKey,
	}
	// Jika password/api_key kosong, pertahankan nilai yang sudah tersimpan
	// agar penyimpanan ulang setelan tidak menghapus rahasia yang ada.
	if cfg.Password == "" || cfg.APIKey == "" {
		if cur, err := s.d.Store.GetEmailSettings(); err == nil {
			if cfg.Password == "" {
				cfg.Password = cur.Password
			}
			if cfg.APIKey == "" {
				cfg.APIKey = cur.APIKey
			}
		}
	}
	if err := s.d.Store.SetEmailSettings(cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menyimpan setelan email")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	if s.d.Mailer == nil {
		writeErr(w, http.StatusServiceUnavailable, "mailer belum dikonfigurasi")
		return
	}
	u, _ := userFrom(r.Context())
	full, err := s.d.Store.GetUserByID(u.ID)
	if err != nil || full == nil || full.Email == "" {
		writeErr(w, http.StatusBadRequest, "akun Anda belum memiliki alamat email")
		return
	}
	if err := s.d.Mailer.Send(full.Email, "Uji Lamund", "<p>Email berfungsi.</p>"); err != nil {
		writeErr(w, http.StatusBadGateway, "gagal kirim email uji: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "terkirim"})
}
