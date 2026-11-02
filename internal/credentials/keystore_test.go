package credentials

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jack-ketch/internal/store"
	"jack-ketch/pkg/api"
)

type fakeKeychain struct {
	secret  []byte
	present bool
	broken  bool
	calls   [][]string
}

func (f *fakeKeychain) runner() cmdRunner {
	return func(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, []byte, int, error) {
		f.calls = append(f.calls, append([]string{name}, args...))
		if f.broken {
			return nil, []byte("Cannot autolaunch D-Bus without X11 $DISPLAY"), 1, nil
		}
		switch args[0] {
		case "lookup":
			for i, a := range args {
				if a == "account" && args[i+1] == "availability-probe" {
					return nil, nil, 1, nil
				}
			}
			if !f.present {
				return nil, nil, 1, nil
			}
			return f.secret, nil, 0, nil
		case "store":
			f.secret = bytes.TrimSpace(stdin)
			f.present = true
			return nil, nil, 0, nil
		case "clear":
			f.present = false
			f.secret = nil
			return nil, nil, 0, nil
		}
		return nil, nil, 2, nil
	}
}

func keychainFor(f *fakeKeychain) *keychainKeystore {
	return &keychainKeystore{run: f.runner(), goos: "linux", look: func(string) (string, error) { return "/usr/bin/secret-tool", nil }}
}

func newManager(t *testing.T, f *fakeKeychain) (*KeyManager, string, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	km := NewKeyManager(dir, db)
	if f != nil {
		km.chain = keychainFor(f)
	} else {
		km.chain = noKeychain()
	}
	return km, dir, db
}

func TestFileBackendCreatesKeyWith0600(t *testing.T) {
	km, dir, _ := newManager(t, nil)
	key, err := km.Key()
	if err != nil || len(key) != 32 {
		t.Fatalf("key: %v %v", key, err)
	}
	info, err := os.Stat(filepath.Join(dir, "credentials.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	again := NewKeyManager(dir, km.settings)
	again.chain = noKeychain()
	got, err := again.Key()
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("key must be stable across restarts")
	}
	st := km.Status()
	if st.Active != BackendFile || st.KeychainAvailable || st.Backend != BackendAuto {
		t.Fatalf("status: %+v", st)
	}
}

func TestAutoPrefersKeychainWhenAvailable(t *testing.T) {
	f := &fakeKeychain{}
	km, dir, _ := newManager(t, f)
	key, err := km.Key()
	if err != nil {
		t.Fatal(err)
	}
	if !f.present || len(f.secret) != 64 {
		t.Fatalf("key should live in the keychain: %+v", f)
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.key")); err == nil {
		t.Fatal("no key file when the keychain holds it")
	}
	if km.Status().Active != BackendKeychain {
		t.Fatalf("status: %+v", km.Status())
	}
	_ = key
}

func TestAutoMigratesExistingFileKeyIntoKeychain(t *testing.T) {
	f := &fakeKeychain{}
	km, dir, db := newManager(t, &fakeKeychain{broken: true})
	original, err := km.Key()
	if err != nil {
		t.Fatal(err)
	}
	if km.Status().Active != BackendFile {
		t.Fatal("starts on file when the keychain is broken")
	}
	upgraded := NewKeyManager(dir, db)
	upgraded.chain = keychainFor(f)
	got, err := upgraded.Key()
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("migration must keep the key: %v", err)
	}
	if !f.present {
		t.Fatal("key should now be in the keychain")
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.key")); err == nil {
		t.Fatal("file key should be removed after migration")
	}
}

func TestNeverRegeneratesWhenKeychainDisappears(t *testing.T) {
	f := &fakeKeychain{}
	km, dir, db := newManager(t, f)
	if _, err := km.Key(); err != nil {
		t.Fatal(err)
	}
	f.broken = true
	next := NewKeyManager(dir, db)
	next.chain = keychainFor(f)
	if _, err := next.Key(); err == nil {
		t.Fatal("must fail instead of silently creating a new key")
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.key")); err == nil {
		t.Fatal("must not create a fresh file key")
	}
}

func TestSetBackendMovesKeyBothWays(t *testing.T) {
	f := &fakeKeychain{}
	km, dir, _ := newManager(t, f)
	if _, err := km.SetBackend(BackendFile); err != nil {
		t.Fatal(err)
	}
	key, _ := km.Key()
	if _, err := os.Stat(filepath.Join(dir, "credentials.key")); err != nil {
		t.Fatal("file backend writes the key file")
	}
	st, err := km.SetBackend(BackendKeychain)
	if err != nil || st.Active != BackendKeychain {
		t.Fatalf("to keychain: %+v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.key")); err == nil {
		t.Fatal("file removed after moving to keychain")
	}
	if !bytes.Equal(f.secret, []byte(strings.TrimSpace(string(f.secret)))) || len(f.secret) != 64 {
		t.Fatal("keychain holds the hex key")
	}
	st, err = km.SetBackend(BackendFile)
	if err != nil || st.Active != BackendFile {
		t.Fatalf("back to file: %+v %v", st, err)
	}
	fresh := NewKeyManager(dir, km.settings)
	fresh.chain = keychainFor(f)
	got, err := fresh.Key()
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("the key must survive both moves")
	}
}

func TestSetBackendKeychainUnavailable(t *testing.T) {
	km, dir, _ := newManager(t, nil)
	key, _ := km.Key()
	if _, err := km.SetBackend(BackendKeychain); err == nil {
		t.Fatal("explicit keychain must fail when unavailable")
	}
	if _, err := km.SetBackend("bogus"); err == nil {
		t.Fatal("bad backend")
	}
	got, err := km.Key()
	if err != nil || !bytes.Equal(got, key) {
		t.Fatal("failed switches change nothing")
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.key")); err != nil {
		t.Fatal("key file must remain")
	}
	if km.Status().KeychainError == "" {
		t.Fatal("status explains why the keychain is unavailable")
	}
}

func TestResetKeyAndSecretsClear(t *testing.T) {
	svc, db, project, dir := newSvc(t)
	v, err := svc.Upsert(project, basicProfile("tok-abcdef-123456"))
	if err != nil {
		t.Fatal(err)
	}
	old, _ := svc.Keys().Key()
	if err := svc.Keys().Reset(); err != nil {
		t.Fatal(err)
	}
	if k, _ := svc.Keys().Key(); bytes.Equal(k, old) {
		t.Fatal("reset must create a new key")
	}
	list, _ := svc.List(project)
	if list[0].Status != "unreadable" {
		t.Fatalf("old secrets can no longer be read: %+v", list[0])
	}
	n, err := svc.ResetSecrets()
	if err != nil || n != 1 {
		t.Fatalf("reset secrets: %d %v", n, err)
	}
	list, _ = svc.List(project)
	if list[0].Status != StatusEmpty {
		t.Fatalf("after reset the profile asks for re-entry: %+v", list[0])
	}
	var tenant string
	for _, f := range list[0].Fields {
		if f.Name == "tenant" {
			tenant = f.Value
		}
	}
	if tenant != "tenant-a" {
		t.Fatal("non-secret values survive a key reset")
	}
	edit := basicProfile("fresh-token-654321")
	edit.ID = v.ID
	if _, err := svc.Upsert(project, edit); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Reveal(project, v.ID, "token"); err != nil || got != "fresh-token-654321" {
		t.Fatalf("re-entry works: %q %v", got, err)
	}
	_ = db
	_ = dir
	_ = api.CredentialField{}
}

func TestKeychainRunnerError(t *testing.T) {
	k := &keychainKeystore{
		run: func(context.Context, []byte, string, ...string) ([]byte, []byte, int, error) {
			return nil, nil, 0, errors.New("boom")
		},
		goos: "darwin",
		look: func(string) (string, error) { return "/usr/bin/security", nil },
	}
	if ok, tool, _ := k.available(); ok || tool != "security" {
		t.Fatal("runner failure means unavailable")
	}
}
