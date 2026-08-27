package store

import "testing"

func TestAppCRUDAndPortAlloc(t *testing.T) {
	st := openTemp(t)
	uid, _ := st.CreateUser(User{Username: "u", PasswordHash: "h"})

	a1, err := st.CreateApp(App{Domain: "app1.com", UserID: uid, Command: "npm start", WorkDir: "/w1", Autostart: true})
	if err != nil {
		t.Fatal(err)
	}
	if a1.Port != portBase {
		t.Fatalf("port pertama harus %d, dapat %d", portBase, a1.Port)
	}
	a2, _ := st.CreateApp(App{Domain: "app2.com", UserID: uid, Command: "node s.js"})
	if a2.Port != portBase+1 {
		t.Fatalf("port kedua harus %d, dapat %d", portBase+1, a2.Port)
	}

	got, _ := st.GetAppByDomain("app1.com")
	if got == nil || got.Command != "npm start" || !got.Autostart || got.WorkDir != "/w1" {
		t.Fatalf("GetApp: %+v", got)
	}

	if _, err := st.CreateApp(App{Domain: "app1.com", UserID: uid, Command: "x"}); err == nil {
		t.Fatal("domain duplikat harus error")
	}

	all, _ := st.ListApps()
	if len(all) != 2 {
		t.Fatalf("ListApps n=%d", len(all))
	}
	mine, _ := st.ListAppsByUser(uid)
	if len(mine) != 2 {
		t.Fatalf("ListAppsByUser n=%d", len(mine))
	}

	if err := st.DeleteApp("app1.com"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetAppByDomain("app1.com"); got != nil {
		t.Fatal("app harus terhapus")
	}
}

func TestAppEnvRoundTrip(t *testing.T) {
	st := openTemp(t)
	uid, _ := st.CreateUser(User{Username: "u", PasswordHash: "h"})
	st.CreateApp(App{Domain: "e.com", UserID: uid, Command: "run", Env: []string{"A=1", "B=2"}})
	got, _ := st.GetAppByDomain("e.com")
	if len(got.Env) != 2 || got.Env[0] != "A=1" || got.Env[1] != "B=2" {
		t.Fatalf("env round-trip salah: %+v", got.Env)
	}
}

func TestAppEnvJSONRoundTrip(t *testing.T) {
	st := openTemp(t)
	uid, _ := st.CreateUser(User{Username: "u", PasswordHash: "h"})
	st.CreateApp(App{Domain: "e2.com", UserID: uid, Command: "run",
		Env: []string{"URL=postgres://a:b@h/db?x=1", "MSG=halo dunia"}})
	got, _ := st.GetAppByDomain("e2.com")
	if len(got.Env) != 2 || got.Env[0] != "URL=postgres://a:b@h/db?x=1" || got.Env[1] != "MSG=halo dunia" {
		t.Fatalf("env round-trip: %+v", got.Env)
	}
	// update
	st.SetAppEnv("e2.com", []string{"ONLY=1"})
	got, _ = st.GetAppByDomain("e2.com")
	if len(got.Env) != 1 || got.Env[0] != "ONLY=1" {
		t.Fatalf("SetAppEnv: %+v", got.Env)
	}
}
