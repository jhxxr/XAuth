package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"xauth/internal/outlook"
	"xauth/internal/totp"
)

// OutlookKeySetting is the settings row that holds the AES-256-GCM key for Outlook credentials.
const OutlookKeySetting = "outlook_key"

// ErrDefaultGroup is returned when attempting to delete the built-in default group (id=1).
var ErrDefaultGroup = errors.New("cannot delete default outlook group")

// ErrUnknownUser is returned when a grant references a user ID that does not exist.
var ErrUnknownUser = errors.New("unknown user")

// OutlookGroup is a group of Outlook mailboxes.
type OutlookGroup struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Color        string    `json:"color"`
	AccountCount int       `json:"accountCount"`
	CreatedAt    time.Time `json:"createdAt"`
}

// OutlookAccount is one stored Outlook mailbox. Password and RefreshToken are
// only populated when requested by an admin detail/export or internal mail fetch.
type OutlookAccount struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Password     string    `json:"password,omitempty"`
	HasPassword  bool      `json:"hasPassword"`
	ClientID     string    `json:"clientId"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	GroupID      int64     `json:"groupId"`
	GroupName    string    `json:"groupName"`
	GroupColor   string    `json:"groupColor"`
	Remark       string    `json:"remark"`
	Status       string    `json:"status"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	UserIDs      []string  `json:"userIds"`
}

// OutlookAccountUpdate holds fields an admin may change on an Outlook account.
type OutlookAccountUpdate struct {
	Email        string
	Password     *string // nil keeps existing password
	ClientID     string
	RefreshToken string // empty keeps existing refresh token
	GroupID      int64
	Remark       *string // nil keeps existing remark
	Status       string
}

// OutlookGrantChange records the user grant diff of one Outlook account.
type OutlookGrantChange struct {
	Email   string
	Added   []string
	Removed []string
}

// UserOutlookMailbox is the non-sensitive view of a granted Outlook mailbox for regular users.
type UserOutlookMailbox struct {
	ID         string    `json:"id"`
	Address    string    `json:"address"`
	Remark     string    `json:"remark"`
	GroupName  string    `json:"groupName"`
	GroupColor string    `json:"groupColor"`
	Status     string    `json:"status"`
	AssignedAt time.Time `json:"assignedAt"`
}

// OutlookKey returns the 32-byte AES-256-GCM encryption key for Outlook credentials.
func (s *Store) OutlookKey(ctx context.Context) ([]byte, error) {
	return s.Setting(ctx, OutlookKeySetting, 32)
}

// ListOutlookGroups returns all Outlook groups with their account counts.
func (s *Store) ListOutlookGroups(ctx context.Context) ([]OutlookGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.name, g.description, g.color, g.created_at,
		       (SELECT COUNT(*) FROM outlook_accounts a WHERE a.group_id = g.id) AS cnt
		FROM outlook_groups g
		ORDER BY CASE WHEN g.id = 1 THEN 0 ELSE 1 END, g.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OutlookGroup{}
	for rows.Next() {
		var g OutlookGroup
		var created int64
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Color, &created, &g.AccountCount); err != nil {
			return nil, err
		}
		g.CreatedAt = fromUnix(created)
		out = append(out, g)
	}
	return out, rows.Err()
}

// CreateOutlookGroup creates a new Outlook mailbox group.
func (s *Store) CreateOutlookGroup(ctx context.Context, name, description, color string) (*OutlookGroup, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	color = strings.TrimSpace(color)
	if color == "" {
		color = "#666666"
	}
	now := s.unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO outlook_groups (name, description, color, created_at) VALUES (?, ?, ?, ?)`,
		name, description, color, now)
	if err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &OutlookGroup{
		ID:          id,
		Name:        name,
		Description: description,
		Color:       color,
		CreatedAt:   fromUnix(now),
	}, nil
}

// UpdateOutlookGroup updates a group's name, description, and color.
func (s *Store) UpdateOutlookGroup(ctx context.Context, id int64, name, description, color string) (*OutlookGroup, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	color = strings.TrimSpace(color)
	if color == "" {
		color = "#666666"
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE outlook_groups SET name = ?, description = ?, color = ? WHERE id = ?`,
		name, description, color, id)
	if err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	var g OutlookGroup
	var created int64
	err = s.db.QueryRowContext(ctx, `
		SELECT g.id, g.name, g.description, g.color, g.created_at,
		       (SELECT COUNT(*) FROM outlook_accounts a WHERE a.group_id = g.id)
		FROM outlook_groups g WHERE g.id = ?`, id).Scan(&g.ID, &g.Name, &g.Description, &g.Color, &created, &g.AccountCount)
	if err != nil {
		return nil, err
	}
	g.CreatedAt = fromUnix(created)
	return &g, nil
}

// DeleteOutlookGroup deletes a non-default group and moves its accounts to default group 1.
func (s *Store) DeleteOutlookGroup(ctx context.Context, id int64) error {
	if id == 1 {
		return ErrDefaultGroup
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE outlook_accounts SET group_id = 1 WHERE group_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM outlook_groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// ImportOutlookAccounts batch-imports parsed Outlook accounts, optionally assigning them to users.
func (s *Store) ImportOutlookAccounts(
	ctx context.Context,
	items []outlook.ParsedAccount,
	groupID int64,
	remark string,
	userIDs []string,
	overwrite bool,
	actorID string,
) (added, updated, skipped int, err error) {
	if len(items) == 0 {
		return 0, 0, 0, nil
	}
	if groupID <= 0 {
		groupID = 1
	}
	var groupExists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outlook_groups WHERE id = ?`, groupID).Scan(&groupExists); err != nil {
		return 0, 0, 0, err
	}
	if groupExists == 0 {
		groupID = 1
	}

	cleanUsers, err := s.validateUserIDs(ctx, userIDs)
	if err != nil {
		return 0, 0, 0, err
	}

	key, err := s.OutlookKey(ctx)
	if err != nil {
		return 0, 0, 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()

	now := s.unix()
	remark = strings.TrimSpace(remark)

	for _, item := range items {
		email := outlook.NormalizeEmail(item.Email)
		var pwdSealed []byte
		if item.Password != "" {
			pwdSealed, err = totp.Encrypt(key, []byte(item.Password))
			if err != nil {
				return 0, 0, 0, err
			}
		} else {
			pwdSealed = []byte{}
		}
		rtSealed, err := totp.Encrypt(key, []byte(item.RefreshToken))
		if err != nil {
			return 0, 0, 0, err
		}

		var existingID string
		err = tx.QueryRowContext(ctx, `SELECT id FROM outlook_accounts WHERE email = ?`, email).Scan(&existingID)
		if errors.Is(err, sql.ErrNoRows) {
			accID := NewID()
			_, err = tx.ExecContext(ctx, `
				INSERT INTO outlook_accounts
				(id, email, password_encrypted, client_id, refresh_token_encrypted, group_id, remark, status, created_by, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?)`,
				accID, email, pwdSealed, item.ClientID, rtSealed, groupID, remark, actorID, now, now)
			if err != nil {
				return 0, 0, 0, err
			}
			for _, uid := range cleanUsers {
				if _, err := tx.ExecContext(ctx, `
					INSERT OR IGNORE INTO outlook_grants (account_id, user_id, assigned_by, assigned_at)
					VALUES (?, ?, ?, ?)`, accID, uid, actorID, now); err != nil {
					return 0, 0, 0, err
				}
			}
			added++
			continue
		}
		if err != nil {
			return 0, 0, 0, err
		}

		if overwrite {
			_, err = tx.ExecContext(ctx, `
				UPDATE outlook_accounts
				SET password_encrypted = CASE WHEN length(?) > 0 THEN ? ELSE password_encrypted END,
				    client_id = ?,
				    refresh_token_encrypted = ?,
				    group_id = ?,
				    remark = CASE WHEN ? != '' THEN ? ELSE remark END,
				    updated_at = ?
				WHERE id = ?`,
				pwdSealed, pwdSealed, item.ClientID, rtSealed, groupID, remark, remark, now, existingID)
			if err != nil {
				return 0, 0, 0, err
			}
			updated++
		} else {
			skipped++
		}

		for _, uid := range cleanUsers {
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO outlook_grants (account_id, user_id, assigned_by, assigned_at)
				VALUES (?, ?, ?, ?)`, existingID, uid, actorID, now); err != nil {
				return 0, 0, 0, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, 0, err
	}
	return added, updated, skipped, nil
}

// ListOutlookAccounts returns stored Outlook accounts (optionally filtered by groupID > 0)
// without exposing passwords or refresh tokens.
func (s *Store) ListOutlookAccounts(ctx context.Context, groupID int64) ([]OutlookAccount, error) {
	q := `
		SELECT a.id, a.email, length(a.password_encrypted) > 0, a.client_id, a.group_id,
		       COALESCE(g.name, '默认分组'), COALESCE(g.color, '#666666'),
		       a.remark, a.status, a.created_by, a.created_at, a.updated_at
		FROM outlook_accounts a
		LEFT JOIN outlook_groups g ON a.group_id = g.id
	`
	var args []any
	if groupID > 0 {
		q += ` WHERE a.group_id = ?`
		args = append(args, groupID)
	}
	q += ` ORDER BY a.created_at DESC, a.email ASC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []OutlookAccount{}
	byID := map[string]int{}
	for rows.Next() {
		var a OutlookAccount
		var created, updated int64
		if err := rows.Scan(
			&a.ID, &a.Email, &a.HasPassword, &a.ClientID, &a.GroupID,
			&a.GroupName, &a.GroupColor, &a.Remark, &a.Status,
			&a.CreatedBy, &created, &updated,
		); err != nil {
			return nil, err
		}
		a.CreatedAt = fromUnix(created)
		a.UpdatedAt = fromUnix(updated)
		a.UserIDs = []string{}
		byID[a.ID] = len(out)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	gRows, err := s.db.QueryContext(ctx, `SELECT account_id, user_id FROM outlook_grants ORDER BY account_id, user_id`)
	if err != nil {
		return nil, err
	}
	defer gRows.Close()
	for gRows.Next() {
		var accID, userID string
		if err := gRows.Scan(&accID, &userID); err != nil {
			return nil, err
		}
		if idx, ok := byID[accID]; ok {
			out[idx].UserIDs = append(out[idx].UserIDs, userID)
		}
	}
	return out, gRows.Err()
}

// OutlookAccountByID returns one account by ID, optionally decrypting its password and refresh token.
func (s *Store) OutlookAccountByID(ctx context.Context, id string, withSecrets bool) (*OutlookAccount, error) {
	return s.queryOneOutlookAccount(ctx, "a.id = ?", id, withSecrets)
}

// OutlookAccountByEmail returns one account by email address, optionally decrypting its secrets.
func (s *Store) OutlookAccountByEmail(ctx context.Context, email string, withSecrets bool) (*OutlookAccount, error) {
	return s.queryOneOutlookAccount(ctx, "a.email = ?", outlook.NormalizeEmail(email), withSecrets)
}

func (s *Store) queryOneOutlookAccount(ctx context.Context, where string, arg any, withSecrets bool) (*OutlookAccount, error) {
	var a OutlookAccount
	var pwdSealed, rtSealed []byte
	var created, updated int64
	err := s.db.QueryRowContext(ctx, `
		SELECT a.id, a.email, a.password_encrypted, a.client_id, a.refresh_token_encrypted,
		       a.group_id, COALESCE(g.name, '默认分组'), COALESCE(g.color, '#666666'),
		       a.remark, a.status, a.created_by, a.created_at, a.updated_at
		FROM outlook_accounts a
		LEFT JOIN outlook_groups g ON a.group_id = g.id
		WHERE `+where, arg).Scan(
		&a.ID, &a.Email, &pwdSealed, &a.ClientID, &rtSealed,
		&a.GroupID, &a.GroupName, &a.GroupColor,
		&a.Remark, &a.Status, &a.CreatedBy, &created, &updated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt = fromUnix(created)
	a.UpdatedAt = fromUnix(updated)
	a.HasPassword = len(pwdSealed) > 0

	if withSecrets {
		key, err := s.OutlookKey(ctx)
		if err != nil {
			return nil, err
		}
		if len(pwdSealed) > 0 {
			plainPwd, err := totp.Decrypt(key, pwdSealed)
			if err != nil {
				return nil, err
			}
			a.Password = string(plainPwd)
		}
		plainRT, err := totp.Decrypt(key, rtSealed)
		if err != nil {
			return nil, err
		}
		a.RefreshToken = string(plainRT)
	}

	a.UserIDs, err = s.outlookGrantUsers(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// UpdateOutlookAccount updates an Outlook account's fields.
func (s *Store) UpdateOutlookAccount(ctx context.Context, id string, in OutlookAccountUpdate) (*OutlookAccount, error) {
	current, err := s.OutlookAccountByID(ctx, id, false)
	if err != nil {
		return nil, err
	}

	email := current.Email
	if in.Email != "" {
		email = outlook.NormalizeEmail(in.Email)
	}
	clientID := current.ClientID
	if strings.TrimSpace(in.ClientID) != "" {
		clientID = strings.TrimSpace(in.ClientID)
	}
	groupID := current.GroupID
	if in.GroupID > 0 && in.GroupID != current.GroupID {
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outlook_groups WHERE id = ?`, in.GroupID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			return nil, ErrNotFound
		}
		groupID = in.GroupID
	}
	status := current.Status
	if in.Status == "active" || in.Status == "disabled" {
		status = in.Status
	}
	remark := current.Remark
	if in.Remark != nil {
		remark = strings.TrimSpace(*in.Remark)
	}

	var pwdSealed []byte
	updatePwd := false
	if in.Password != nil {
		updatePwd = true
		p := strings.TrimSpace(*in.Password)
		if p != "" {
			key, err := s.OutlookKey(ctx)
			if err != nil {
				return nil, err
			}
			pwdSealed, err = totp.Encrypt(key, []byte(p))
			if err != nil {
				return nil, err
			}
		} else {
			pwdSealed = []byte{}
		}
	}

	var rtSealed []byte
	if strings.TrimSpace(in.RefreshToken) != "" {
		key, err := s.OutlookKey(ctx)
		if err != nil {
			return nil, err
		}
		rtSealed, err = totp.Encrypt(key, []byte(strings.TrimSpace(in.RefreshToken)))
		if err != nil {
			return nil, err
		}
	}

	now := s.unix()
	_, err = s.db.ExecContext(ctx, `
		UPDATE outlook_accounts
		SET email = ?,
		    password_encrypted = CASE WHEN ? = 1 THEN ? ELSE password_encrypted END,
		    client_id = ?,
		    refresh_token_encrypted = CASE WHEN ? IS NOT NULL THEN ? ELSE refresh_token_encrypted END,
		    group_id = ?,
		    remark = ?,
		    status = ?,
		    updated_at = ?
		WHERE id = ?`,
		email, boolInt(updatePwd), pwdSealed, clientID, rtSealed, rtSealed, groupID, remark, status, now, id)
	if err != nil {
		if isUnique(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return s.OutlookAccountByID(ctx, id, false)
}

// DeleteOutlookAccounts removes one or more Outlook accounts (and their grants via ON DELETE CASCADE).
func (s *Store) DeleteOutlookAccounts(ctx context.Context, ids []string) ([]string, error) {
	clean := cleanStringSlice(ids)
	if len(clean) == 0 {
		return nil, nil
	}
	placeholders := "?" + strings.Repeat(", ?", len(clean)-1)
	args := make([]any, len(clean))
	for i, id := range clean {
		args[i] = id
	}

	rows, err := s.db.QueryContext(ctx, "SELECT email FROM outlook_accounts WHERE id IN ("+placeholders+")", args...)
	if err != nil {
		return nil, err
	}
	var emails []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return nil, err
		}
		emails = append(emails, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if _, err := s.db.ExecContext(ctx, "DELETE FROM outlook_accounts WHERE id IN ("+placeholders+")", args...); err != nil {
		return nil, err
	}
	return emails, nil
}

// ExportOutlookAccounts decrypts and returns accounts matching the optional groupIDs or accountIDs.
func (s *Store) ExportOutlookAccounts(ctx context.Context, groupIDs []int64, accountIDs []string) ([]OutlookAccount, error) {
	q := `SELECT id, email, password_encrypted, client_id, refresh_token_encrypted, group_id, remark, status FROM outlook_accounts WHERE 1=1`
	var args []any
	if len(groupIDs) > 0 {
		q += ` AND group_id IN (?` + strings.Repeat(", ?", len(groupIDs)-1) + `)`
		for _, gid := range groupIDs {
			args = append(args, gid)
		}
	}
	cleanIDs := cleanStringSlice(accountIDs)
	if len(cleanIDs) > 0 {
		q += ` AND id IN (?` + strings.Repeat(", ?", len(cleanIDs)-1) + `)`
		for _, id := range cleanIDs {
			args = append(args, id)
		}
	}
	q += ` ORDER BY group_id ASC, created_at DESC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	key, err := s.OutlookKey(ctx)
	if err != nil {
		return nil, err
	}

	out := []OutlookAccount{}
	for rows.Next() {
		var a OutlookAccount
		var pwdSealed, rtSealed []byte
		if err := rows.Scan(&a.ID, &a.Email, &pwdSealed, &a.ClientID, &rtSealed, &a.GroupID, &a.Remark, &a.Status); err != nil {
			return nil, err
		}
		if len(pwdSealed) > 0 {
			if p, err := totp.Decrypt(key, pwdSealed); err == nil {
				a.Password = string(p)
			}
		}
		if rt, err := totp.Decrypt(key, rtSealed); err == nil {
			a.RefreshToken = string(rt)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetOutlookGrants replaces the users granted access to one Outlook account.
func (s *Store) SetOutlookGrants(ctx context.Context, accountID string, userIDs []string, assignedBy string) (added, removed []string, email string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT email FROM outlook_accounts WHERE id = ?`, accountID).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, "", ErrNotFound
	}
	if err != nil {
		return nil, nil, "", err
	}

	cleanUsers, err := s.validateUserIDs(ctx, userIDs)
	if err != nil {
		return nil, nil, "", err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, "", err
	}
	defer tx.Rollback()

	current := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT user_id FROM outlook_grants WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, nil, "", err
	}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return nil, nil, "", err
		}
		current[uid] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, "", err
	}

	want := map[string]bool{}
	now := s.unix()
	for _, uid := range cleanUsers {
		want[uid] = true
		if current[uid] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO outlook_grants (account_id, user_id, assigned_by, assigned_at)
			VALUES (?, ?, ?, ?)`, accountID, uid, assignedBy, now); err != nil {
			return nil, nil, "", err
		}
		added = append(added, uid)
	}
	for uid := range current {
		if want[uid] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM outlook_grants WHERE account_id = ? AND user_id = ?`, accountID, uid); err != nil {
			return nil, nil, "", err
		}
		removed = append(removed, uid)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, "", err
	}
	return added, removed, email, nil
}

// BatchSetOutlookGrants assigns multiple Outlook accounts to users in "replace" or "append" mode.
func (s *Store) BatchSetOutlookGrants(ctx context.Context, accountIDs, userIDs []string, mode, assignedBy string) ([]OutlookGrantChange, error) {
	cleanAccs := cleanStringSlice(accountIDs)
	if len(cleanAccs) == 0 {
		return nil, nil
	}
	cleanUsers, err := s.validateUserIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := s.unix()
	var changes []OutlookGrantChange
	for _, accID := range cleanAccs {
		var email string
		if err := tx.QueryRowContext(ctx, `SELECT email FROM outlook_accounts WHERE id = ?`, accID).Scan(&email); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}

		current := map[string]bool{}
		rows, err := tx.QueryContext(ctx, `SELECT user_id FROM outlook_grants WHERE account_id = ?`, accID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var uid string
			if err := rows.Scan(&uid); err != nil {
				rows.Close()
				return nil, err
			}
			current[uid] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}

		change := OutlookGrantChange{Email: email}
		want := map[string]bool{}
		for _, uid := range cleanUsers {
			want[uid] = true
			if current[uid] {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO outlook_grants (account_id, user_id, assigned_by, assigned_at)
				VALUES (?, ?, ?, ?)`, accID, uid, assignedBy, now); err != nil {
				return nil, err
			}
			change.Added = append(change.Added, uid)
		}
		if mode != "append" {
			for uid := range current {
				if want[uid] {
					continue
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM outlook_grants WHERE account_id = ? AND user_id = ?`, accID, uid); err != nil {
					return nil, err
				}
				change.Removed = append(change.Removed, uid)
			}
		}
		changes = append(changes, change)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return changes, nil
}

// BatchMoveOutlookGroup moves multiple Outlook accounts to a target group.
func (s *Store) BatchMoveOutlookGroup(ctx context.Context, accountIDs []string, groupID int64) (int, error) {
	cleanAccs := cleanStringSlice(accountIDs)
	if len(cleanAccs) == 0 {
		return 0, nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outlook_groups WHERE id = ?`, groupID).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, ErrNotFound
	}
	placeholders := "?" + strings.Repeat(", ?", len(cleanAccs)-1)
	args := make([]any, 0, len(cleanAccs)+2)
	args = append(args, groupID, s.unix())
	for _, id := range cleanAccs {
		args = append(args, id)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE outlook_accounts SET group_id = ?, updated_at = ? WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// UserOutlookMailboxes returns the Outlook mailboxes granted to a specific user.
func (s *Store) UserOutlookMailboxes(ctx context.Context, userID string) ([]UserOutlookMailbox, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.email, a.remark, COALESCE(grp.name, '默认分组'), COALESCE(grp.color, '#666666'), a.status, g.assigned_at
		FROM outlook_grants g
		JOIN outlook_accounts a ON a.id = g.account_id
		LEFT JOIN outlook_groups grp ON grp.id = a.group_id
		WHERE g.user_id = ?
		ORDER BY a.email ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserOutlookMailbox{}
	for rows.Next() {
		var m UserOutlookMailbox
		var assignedAt int64
		if err := rows.Scan(&m.ID, &m.Address, &m.Remark, &m.GroupName, &m.GroupColor, &m.Status, &assignedAt); err != nil {
			return nil, err
		}
		m.AssignedAt = fromUnix(assignedAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

// UserGrantedOutlookAccount checks that userID is granted access to address and
// returns the account with decrypted OAuth2 credentials so the server can fetch mail.
// Returns ErrNotFound when the mailbox does not exist or is not granted to userID.
func (s *Store) UserGrantedOutlookAccount(ctx context.Context, userID, address string) (*OutlookAccount, error) {
	email := outlook.NormalizeEmail(address)
	var accID string
	err := s.db.QueryRowContext(ctx, `
		SELECT a.id
		FROM outlook_accounts a
		JOIN outlook_grants g ON g.account_id = a.id
		WHERE a.email = ? AND g.user_id = ?
	`, email, userID).Scan(&accID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.OutlookAccountByID(ctx, accID, true)
}

func (s *Store) outlookGrantUsers(ctx context.Context, accountID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM outlook_grants WHERE account_id = ? ORDER BY user_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

func (s *Store) validateUserIDs(ctx context.Context, userIDs []string) ([]string, error) {
	clean := cleanStringSlice(userIDs)
	if len(clean) == 0 {
		return []string{}, nil
	}
	q := "SELECT COUNT(*) FROM users WHERE id IN (?" + strings.Repeat(", ?", len(clean)-1) + ")"
	args := make([]any, len(clean))
	for i, id := range clean {
		args[i] = id
	}
	var n int
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return nil, err
	}
	if n != len(clean) {
		return nil, ErrUnknownUser
	}
	return clean, nil
}

func cleanStringSlice(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
