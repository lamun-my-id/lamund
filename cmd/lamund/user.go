package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/store"
	"golang.org/x/term"
)

func cmdUser(args []string) {
	if len(args) < 1 {
		fatal("usage: lamund user <create|list> ...")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("user "+sub, flag.ExitOnError)
	dbPath := fs.String("db", "/var/lib/lamund/lamund.db", "path database SQLite")
	username := fs.String("username", "", "nama pengguna")
	admin := fs.Bool("admin", false, "jadikan admin")
	password := fs.String("password", "", "password (kosong = tanya lewat prompt)")
	maxSites := fs.Int("max-sites", 0, "kuota jumlah situs (0 = default)")
	fs.Parse(rest)

	switch sub {
	case "create":
		must(userCreate(*dbPath, *username, *password, *admin, *maxSites))
	case "list":
		must(userList(*dbPath))
	default:
		fatal("subperintah user tidak dikenal: %s", sub)
	}
}

func userCreate(dbPath, username, password string, admin bool, maxSites int) error {
	if len(username) < 2 {
		return fmt.Errorf("username minimal 2 karakter")
	}
	if password == "" {
		pw, err := promptPassword()
		if err != nil {
			return err
		}
		password = pw
	}
	if len(password) < 8 {
		return fmt.Errorf("password minimal 8 karakter")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	role := "member"
	if admin {
		role = "superadmin"
	}
	id, err := st.CreateUser(store.User{Username: username, PasswordHash: hash, Role: role})
	if err != nil {
		return err
	}
	if maxSites > 0 {
		if err := st.SetQuota(store.Quota{UserID: id, MaxSites: maxSites}); err != nil {
			return err
		}
	}
	fmt.Printf("✔ user %s (%s) dibuat, id=%d\n", username, role, id)
	return nil
}

func userList(dbPath string) error {
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	users, err := st.ListUsers()
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("(belum ada user)")
		return nil
	}
	for _, u := range users {
		state := "aktif"
		if u.Disabled {
			state = "nonaktif"
		}
		fmt.Printf("%3d  %-20s %-6s %s\n", u.ID, u.Username, u.Role, state)
	}
	return nil
}

// promptPassword membaca password dua kali tanpa echo.
func promptPassword() (string, error) {
	fmt.Fprint(os.Stderr, "Password: ")
	p1, err := term.ReadPassword(syscall.Stdin)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Ulangi password: ")
	p2, err := term.ReadPassword(syscall.Stdin)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(p1)) != strings.TrimSpace(string(p2)) {
		return "", fmt.Errorf("password tidak cocok")
	}
	return strings.TrimSpace(string(p1)), nil
}
