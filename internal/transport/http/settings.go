package http

import (
	"net/http"

	"onobill/internal/config"
)

// ============ GANTI PASSWORD ============

// ChangePasswordPage menampilkan form ganti password (untuk semua user, termasuk superadmin).
func (h *Handler) ChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	render(w, "change_password", map[string]interface{}{
		"Title":   "Ganti Password - ONOBILL",
		"Page":    "change_password",
		"Email":   r.Header.Get("X-User-Email"),
		"Success": r.URL.Query().Get("success"),
		"Error":   r.URL.Query().Get("error"),
	})
}

// ChangePasswordSubmit memproses ganti password: verifikasi password lama, set password baru.
func (h *Handler) ChangePasswordSubmit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	email := r.Header.Get("X-User-Email")
	oldPass := r.FormValue("old_password")
	newPass := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	if newPass == "" || newPass != confirm {
		http.Redirect(w, r, "/settings/password?error=konfirmasi+password+tidak+cocok", http.StatusSeeOther)
		return
	}
	if len(newPass) < 4 {
		http.Redirect(w, r, "/settings/password?error=password+minimal+4+karakter", http.StatusSeeOther)
		return
	}

	user, err := h.Repo.GetUserByEmail(email)
	if err != nil {
		http.Redirect(w, r, "/settings/password?error=user+tidak+ditemukan", http.StatusSeeOther)
		return
	}
	if !config.CheckPassword(user.PasswordHash, oldPass) {
		http.Redirect(w, r, "/settings/password?error=password+lama+salah", http.StatusSeeOther)
		return
	}
	hash, err := config.HashPassword(newPass)
	if err != nil {
		http.Redirect(w, r, "/settings/password?error=gagal+hash", http.StatusSeeOther)
		return
	}
	user.PasswordHash = hash
	if err := h.Repo.UpdateUser(user); err != nil {
		http.Redirect(w, r, "/settings/password?error=gagal+simpan", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/password?success=1", http.StatusSeeOther)
}
