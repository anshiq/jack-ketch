package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type CredentialRow struct {
	ID            string
	ProjectID     string
	Name          string
	Description   string
	Environment   string
	IsDefault     bool
	AppsJSON      string
	FieldsJSON    string
	RecipeJSON    string
	JarJSON       string
	CaptureJar    bool
	CreatedAt     int64
	UpdatedAt     int64
	LastUsedAt    int64
	LastRefreshAt int64
	LastError     string
}

var ErrCredentialNotFound = errors.New("credential profile not found")

const credentialColumns = `id, project_id, name, description, environment, is_default, apps_json, fields_json, recipe_json, jar_json, capture_jar, created_at, updated_at, last_used_at, last_refresh_at, last_error`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCredential(sc rowScanner) (*CredentialRow, error) {
	var r CredentialRow
	var def, capture int
	var used, refreshed sql.NullInt64
	if err := sc.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Description, &r.Environment, &def, &r.AppsJSON, &r.FieldsJSON, &r.RecipeJSON, &r.JarJSON, &capture, &r.CreatedAt, &r.UpdatedAt, &used, &refreshed, &r.LastError); err != nil {
		return nil, err
	}
	r.IsDefault = def != 0
	r.CaptureJar = capture != 0
	r.LastUsedAt = used.Int64
	r.LastRefreshAt = refreshed.Int64
	return &r, nil
}

func (d *DB) ListCredentials(projectID string) ([]*CredentialRow, error) {
	rows, err := d.db.Query(`SELECT `+credentialColumns+` FROM credential_profiles WHERE project_id=? ORDER BY is_default DESC, name COLLATE NOCASE`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CredentialRow
	for rows.Next() {
		r, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) GetCredential(projectID, name string) (*CredentialRow, error) {
	r, err := scanCredential(d.db.QueryRow(`SELECT `+credentialColumns+` FROM credential_profiles WHERE project_id=? AND name=?`, projectID, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCredentialNotFound
	}
	return r, err
}

func (d *DB) GetCredentialByID(id string) (*CredentialRow, error) {
	r, err := scanCredential(d.db.QueryRow(`SELECT `+credentialColumns+` FROM credential_profiles WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCredentialNotFound
	}
	return r, err
}

func (d *DB) DefaultCredential(projectID string) (*CredentialRow, error) {
	r, err := scanCredential(d.db.QueryRow(`SELECT `+credentialColumns+` FROM credential_profiles WHERE project_id=? AND is_default=1 LIMIT 1`, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCredentialNotFound
	}
	return r, err
}

func b2i(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (d *DB) PutCredential(r *CredentialRow) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	if r.IsDefault {
		if _, err := tx.Exec(`UPDATE credential_profiles SET is_default=0 WHERE project_id=? AND id<>?`, r.ProjectID, r.ID); err != nil {
			return err
		}
	}
	var clash string
	err = tx.QueryRow(`SELECT id FROM credential_profiles WHERE project_id=? AND name=? AND id<>?`, r.ProjectID, r.Name, r.ID).Scan(&clash)
	if err == nil {
		return fmt.Errorf("a profile named %q already exists in this project", r.Name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(`INSERT INTO credential_profiles (`+credentialColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description, environment=excluded.environment,
		is_default=excluded.is_default, apps_json=excluded.apps_json, fields_json=excluded.fields_json, recipe_json=excluded.recipe_json,
		jar_json=excluded.jar_json, capture_jar=excluded.capture_jar, updated_at=excluded.updated_at, last_error=excluded.last_error`,
		r.ID, r.ProjectID, r.Name, r.Description, r.Environment, b2i(r.IsDefault), r.AppsJSON, r.FieldsJSON, r.RecipeJSON, r.JarJSON, b2i(r.CaptureJar),
		r.CreatedAt, r.UpdatedAt, nullInt(r.LastUsedAt), nullInt(r.LastRefreshAt), r.LastError)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func (d *DB) DeleteCredential(id string) error {
	res, err := d.db.Exec(`DELETE FROM credential_profiles WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

func (d *DB) TouchCredential(id string) error {
	_, err := d.db.Exec(`UPDATE credential_profiles SET last_used_at=? WHERE id=?`, time.Now().Unix(), id)
	return err
}

func (d *DB) UpdateCredentialState(id, fieldsJSON, jarJSON, lastError string, refreshed bool) error {
	now := time.Now().Unix()
	if refreshed {
		_, err := d.db.Exec(`UPDATE credential_profiles SET fields_json=?, jar_json=?, last_error=?, last_refresh_at=?, updated_at=? WHERE id=?`, fieldsJSON, jarJSON, lastError, now, now, id)
		return err
	}
	_, err := d.db.Exec(`UPDATE credential_profiles SET fields_json=?, jar_json=?, last_error=?, updated_at=? WHERE id=?`, fieldsJSON, jarJSON, lastError, now, id)
	return err
}

func (d *DB) SetCredentialError(id, lastError string) error {
	_, err := d.db.Exec(`UPDATE credential_profiles SET last_error=? WHERE id=?`, lastError, id)
	return err
}
