package store

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const sshHostSelectCols = `id, user_id, name, host, port, username, auth_type, secret_enc, host_key, default_cwd, enabled, created_at, updated_at`

func (d *DBStore) ListSSHHosts(ctx context.Context, userID string) ([]SSHHostRecord, error) {
	rows, err := d.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT `+sshHostSelectCols+` FROM ssh_hosts WHERE user_id = %s ORDER BY name`, d.ph(1)),
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SSHHostRecord
	for rows.Next() {
		h, err := scanSSHHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

func (d *DBStore) GetSSHHost(ctx context.Context, id string) (*SSHHostRecord, error) {
	row := d.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT `+sshHostSelectCols+` FROM ssh_hosts WHERE id = %s`, d.ph(1)), id)
	h, err := scanSSHHost(row)
	if err != nil {
		return nil, scanErr(err)
	}
	return h, nil
}

func (d *DBStore) GetSSHHostByName(ctx context.Context, userID, name string) (*SSHHostRecord, error) {
	row := d.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT `+sshHostSelectCols+` FROM ssh_hosts WHERE user_id = %s AND name = %s`, d.ph(1), d.ph(2)),
		userID, name)
	h, err := scanSSHHost(row)
	if err != nil {
		return nil, scanErr(err)
	}
	return h, nil
}

func (d *DBStore) SaveSSHHost(ctx context.Context, h *SSHHostRecord) error {
	if h == nil {
		return errors.New("store: SaveSSHHost requires a host")
	}
	if strings.TrimSpace(h.UserID) == "" {
		return errors.New("store: SaveSSHHost requires userId")
	}
	if strings.TrimSpace(h.Name) == "" {
		return errors.New("store: SaveSSHHost requires name")
	}
	if strings.TrimSpace(h.Host) == "" {
		return errors.New("store: SaveSSHHost requires host")
	}
	if strings.TrimSpace(h.Username) == "" {
		return errors.New("store: SaveSSHHost requires username")
	}
	if h.AuthType != SSHAuthKey && h.AuthType != SSHAuthPassword {
		return errors.New("store: SaveSSHHost authType must be key or password")
	}
	if h.Port <= 0 {
		h.Port = 22
	}
	now := time.Now().UTC()
	if h.CreatedAt.IsZero() {
		h.CreatedAt = now
	}
	h.UpdatedAt = now
	if h.ID == "" {
		h.ID = randomSSHHostID()
	}

	if existing, err := d.GetSSHHostByName(ctx, h.UserID, h.Name); err == nil && existing != nil && existing.ID != h.ID {
		return ErrSSHHostNameTaken
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	enabledInt := 0
	if h.Enabled {
		enabledInt = 1
	}
	if d.dialect == "postgres" {
		_, err := d.db.ExecContext(ctx,
			`INSERT INTO ssh_hosts (id, user_id, name, host, port, username, auth_type, secret_enc, host_key, default_cwd, enabled, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			 ON CONFLICT (id) DO UPDATE SET
			   name=EXCLUDED.name, host=EXCLUDED.host, port=EXCLUDED.port, username=EXCLUDED.username,
			   auth_type=EXCLUDED.auth_type, secret_enc=EXCLUDED.secret_enc, host_key=EXCLUDED.host_key,
			   default_cwd=EXCLUDED.default_cwd, enabled=EXCLUDED.enabled, updated_at=EXCLUDED.updated_at`,
			h.ID, h.UserID, h.Name, h.Host, h.Port, h.Username, h.AuthType, h.SecretEnc, h.HostKey, h.DefaultCWD, enabledInt, h.CreatedAt, h.UpdatedAt)
		return err
	}
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO ssh_hosts (id, user_id, name, host, port, username, auth_type, secret_enc, host_key, default_cwd, enabled, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT (id) DO UPDATE SET
		   name=excluded.name, host=excluded.host, port=excluded.port, username=excluded.username,
		   auth_type=excluded.auth_type, secret_enc=excluded.secret_enc, host_key=excluded.host_key,
		   default_cwd=excluded.default_cwd, enabled=excluded.enabled, updated_at=excluded.updated_at`,
		h.ID, h.UserID, h.Name, h.Host, h.Port, h.Username, h.AuthType, h.SecretEnc, h.HostKey, h.DefaultCWD, enabledInt, h.CreatedAt, h.UpdatedAt)
	return err
}

func (d *DBStore) DeleteSSHHost(ctx context.Context, id string) error {
	res, err := d.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM ssh_hosts WHERE id = %s`, d.ph(1)), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func randomSSHHostID() string {
	var b [10]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (i * 8))
		}
	}
	return "ssh_" + hex.EncodeToString(b[:])
}

func scanSSHHost(row rowScanner) (*SSHHostRecord, error) {
	var h SSHHostRecord
	var enabledInt int
	if err := row.Scan(
		&h.ID, &h.UserID, &h.Name, &h.Host, &h.Port, &h.Username, &h.AuthType,
		&h.SecretEnc, &h.HostKey, &h.DefaultCWD, &enabledInt, &h.CreatedAt, &h.UpdatedAt,
	); err != nil {
		return nil, err
	}
	h.Enabled = enabledInt != 0
	return &h, nil
}
