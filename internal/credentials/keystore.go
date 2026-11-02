package credentials

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"jack-ketch/pkg/api"
)

const (
	BackendAuto     = "auto"
	BackendFile     = "file"
	BackendKeychain = "keychain"

	settingBackend = "credential_keystore"
	settingActive  = "credential_keystore_active"
	keychainSvc    = "jack-ketch"
	keychainAcct   = "credentials-key"
)

var (
	ErrKeyNotFound    = errors.New("credentials key not found")
	ErrKeyUnavailable = errors.New("credentials key is unavailable")
)

type SettingStore interface {
	GetSetting(key string) (string, bool, error)
	SetSetting(key, valueJSON string) error
}

type keystore interface {
	name() string
	load() ([]byte, error)
	store(key []byte) error
	remove() error
}

type fileKeystore struct{ path string }

func (f *fileKeystore) name() string { return BackendFile }

func (f *fileKeystore) load() ([]byte, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeKey(string(data))
}

func (f *fileKeystore) store(key []byte) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (f *fileKeystore) remove() error {
	if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func decodeKey(s string) ([]byte, error) {
	key, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil || len(key) != 32 {
		return nil, errors.New("stored credentials key is malformed")
	}
	return key, nil
}

type cmdRunner func(ctx context.Context, stdin []byte, name string, args ...string) (stdout, stderr []byte, code int, err error)

func execRunner(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
		err = nil
	}
	return out.Bytes(), errb.Bytes(), code, err
}

type keychainKeystore struct {
	run  cmdRunner
	goos string
	look func(string) (string, error)
}

func newKeychain() *keychainKeystore {
	return &keychainKeystore{run: execRunner, goos: runtime.GOOS, look: exec.LookPath}
}

func (k *keychainKeystore) name() string { return BackendKeychain }

func (k *keychainKeystore) tool() string {
	switch k.goos {
	case "linux":
		return "secret-tool"
	case "darwin":
		return "security"
	}
	return ""
}

func (k *keychainKeystore) available() (bool, string, error) {
	if k.goos == "disabled" {
		return false, "", errors.New("the OS keychain is disabled")
	}
	tool := k.tool()
	if tool == "" {
		return false, "", fmt.Errorf("no OS keychain integration for %s", k.goos)
	}
	if _, err := k.look(tool); err != nil {
		return false, tool, fmt.Errorf("%s is not installed", tool)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	switch k.goos {
	case "linux":
		_, stderr, code, err := k.run(ctx, nil, tool, "lookup", "service", keychainSvc, "account", "availability-probe")
		if err != nil {
			return false, tool, err
		}
		if code > 1 || len(bytes.TrimSpace(stderr)) > 0 {
			msg := strings.TrimSpace(string(stderr))
			if msg == "" {
				msg = fmt.Sprintf("secret-tool exited with %d", code)
			}
			return false, tool, errors.New(msg)
		}
	case "darwin":
		_, stderr, code, err := k.run(ctx, nil, tool, "find-generic-password", "-s", keychainSvc, "-a", "availability-probe")
		if err != nil {
			return false, tool, err
		}
		if code != 0 && code != 44 {
			return false, tool, fmt.Errorf("security: %s", strings.TrimSpace(string(stderr)))
		}
	}
	return true, tool, nil
}

func (k *keychainKeystore) load() ([]byte, error) {
	if ok, _, err := k.available(); !ok {
		return nil, fmt.Errorf("%w: %v", ErrKeyUnavailable, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out, stderr []byte
	var code int
	var err error
	switch k.goos {
	case "linux":
		out, stderr, code, err = k.run(ctx, nil, "secret-tool", "lookup", "service", keychainSvc, "account", keychainAcct)
		if err == nil && code == 1 && len(bytes.TrimSpace(out)) == 0 {
			return nil, ErrKeyNotFound
		}
	case "darwin":
		out, stderr, code, err = k.run(ctx, nil, "security", "find-generic-password", "-s", keychainSvc, "-a", keychainAcct, "-w")
		if err == nil && code == 44 {
			return nil, ErrKeyNotFound
		}
	default:
		return nil, ErrKeyUnavailable
	}
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("%w: %s", ErrKeyUnavailable, strings.TrimSpace(string(stderr)))
	}
	return decodeKey(string(out))
}

func (k *keychainKeystore) store(key []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	encoded := hex.EncodeToString(key)
	var stderr []byte
	var code int
	var err error
	switch k.goos {
	case "linux":
		_, stderr, code, err = k.run(ctx, []byte(encoded), "secret-tool", "store", "--label=jack-ketch credentials key", "service", keychainSvc, "account", keychainAcct)
	case "darwin":
		_, stderr, code, err = k.run(ctx, nil, "security", "add-generic-password", "-U", "-s", keychainSvc, "-a", keychainAcct, "-w", encoded)
	default:
		return ErrKeyUnavailable
	}
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("keychain store failed: %s", strings.TrimSpace(string(stderr)))
	}
	return nil
}

func (k *keychainKeystore) remove() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch k.goos {
	case "linux":
		_, _, _, err := k.run(ctx, nil, "secret-tool", "clear", "service", keychainSvc, "account", keychainAcct)
		return err
	case "darwin":
		_, _, _, err := k.run(ctx, nil, "security", "delete-generic-password", "-s", keychainSvc, "-a", keychainAcct)
		return err
	}
	return nil
}

type KeyManager struct {
	settings SettingStore
	file     *fileKeystore
	chain    *keychainKeystore

	mu       sync.Mutex
	key      []byte
	availAt  time.Time
	availOK  bool
	availErr error
	availCmd string
}

func NewKeyManager(dataDir string, settings SettingStore) *KeyManager {
	m := &KeyManager{settings: settings, file: &fileKeystore{path: filepath.Join(dataDir, "credentials.key")}, chain: newKeychain()}
	if os.Getenv("JACK_KETCH_NO_KEYCHAIN") != "" {
		m.UseKeychain(false)
	}
	return m
}

func (m *KeyManager) UseKeychain(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if enabled {
		m.chain = newKeychain()
	} else {
		m.chain = &keychainKeystore{run: execRunner, goos: "disabled", look: exec.LookPath}
	}
	m.availAt = time.Time{}
}

func (m *KeyManager) setting(key, def string) string {
	raw, ok, err := m.settings.GetSetting(key)
	if err != nil || !ok {
		return def
	}
	v := strings.Trim(strings.TrimSpace(raw), `"`)
	if v == "" {
		return def
	}
	return v
}

func (m *KeyManager) persist(key, value string) error {
	return m.settings.SetSetting(key, `"`+value+`"`)
}

func (m *KeyManager) keychainState() (bool, string, error) {
	if time.Since(m.availAt) < 30*time.Second && !m.availAt.IsZero() {
		return m.availOK, m.availCmd, m.availErr
	}
	m.availOK, m.availCmd, m.availErr = m.chain.available()
	m.availAt = time.Now()
	return m.availOK, m.availCmd, m.availErr
}

func (m *KeyManager) resolve(backend string) (keystore, error) {
	switch backend {
	case BackendFile:
		return m.file, nil
	case BackendKeychain:
		if ok, _, err := m.keychainState(); !ok {
			return nil, fmt.Errorf("the OS keychain is not available: %v", err)
		}
		return m.chain, nil
	default:
		if ok, _, _ := m.keychainState(); ok {
			return m.chain, nil
		}
		return m.file, nil
	}
}

func (m *KeyManager) store(name string) keystore {
	if name == BackendKeychain {
		return m.chain
	}
	return m.file
}

func (m *KeyManager) activeName() string {
	active := m.setting(settingActive, "")
	if active != "" {
		return active
	}
	if _, err := m.file.load(); err == nil {
		return BackendFile
	}
	return ""
}

func (m *KeyManager) Key() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.key != nil {
		return m.key, nil
	}
	backend := m.setting(settingBackend, BackendAuto)
	active := m.activeName()
	if active == "" {
		target, err := m.resolve(backend)
		if err != nil {
			target = m.file
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := target.store(key); err != nil {
			return nil, err
		}
		if err := m.persist(settingActive, target.name()); err != nil {
			return nil, err
		}
		m.key = key
		return key, nil
	}
	key, err := m.store(active).load()
	if err != nil {
		return nil, err
	}
	if active == BackendFile && backend == BackendAuto {
		if ok, _, _ := m.keychainState(); ok {
			if m.chain.store(key) == nil {
				if got, err := m.chain.load(); err == nil && bytes.Equal(got, key) {
					_ = m.persist(settingActive, BackendKeychain)
					_ = m.file.remove()
				}
			}
		}
	}
	m.key = key
	return key, nil
}

func (m *KeyManager) Status() api.KeystoreStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	ok, tool, err := m.keychainState()
	st := api.KeystoreStatus{
		Backend:           m.setting(settingBackend, BackendAuto),
		Active:            m.activeName(),
		KeychainAvailable: ok,
		KeychainTool:      tool,
		KeyPath:           m.file.path,
	}
	if err != nil {
		st.KeychainError = err.Error()
	}
	if st.Active == "" {
		st.Active = "none"
	}
	return st
}

func (m *KeyManager) SetBackend(backend string) (api.KeystoreStatus, error) {
	if backend != BackendAuto && backend != BackendFile && backend != BackendKeychain {
		return api.KeystoreStatus{}, fmt.Errorf("backend must be auto, file or keychain")
	}
	m.mu.Lock()
	m.availAt = time.Time{}
	active := m.activeName()
	var key []byte
	if active != "" {
		k, err := m.store(active).load()
		if err != nil {
			m.mu.Unlock()
			return api.KeystoreStatus{}, err
		}
		key = k
	}
	target, err := m.resolve(backend)
	if err != nil {
		m.mu.Unlock()
		return api.KeystoreStatus{}, err
	}
	if key != nil && target.name() != active {
		if err := target.store(key); err != nil {
			m.mu.Unlock()
			return api.KeystoreStatus{}, err
		}
		got, err := target.load()
		if err != nil || !bytes.Equal(got, key) {
			m.mu.Unlock()
			return api.KeystoreStatus{}, errors.New("the key could not be verified in the new backend; nothing was changed")
		}
		if err := m.persist(settingActive, target.name()); err != nil {
			m.mu.Unlock()
			return api.KeystoreStatus{}, err
		}
		_ = m.store(active).remove()
	}
	if err := m.persist(settingBackend, backend); err != nil {
		m.mu.Unlock()
		return api.KeystoreStatus{}, err
	}
	m.mu.Unlock()
	return m.Status(), nil
}

func (m *KeyManager) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	backend := m.setting(settingBackend, BackendAuto)
	target, err := m.resolve(backend)
	if err != nil {
		target = m.file
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if err := target.store(key); err != nil {
		return err
	}
	if err := m.persist(settingActive, target.name()); err != nil {
		return err
	}
	other := m.store(BackendFile)
	if target.name() == BackendFile {
		other = m.chain
	}
	_ = other.remove()
	m.key = key
	return nil
}

func (m *KeyManager) Probe() error {
	_, err := m.Key()
	return err
}
