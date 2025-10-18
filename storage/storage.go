package storage

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type User struct {
	UserID     uint64
	Hinted     int
	NextHintAt time.Time
}

type Storage interface {
	TryResetQuotaOnNextDay(userID, channelID uint64) error
	ResetQuotaUsage(userID, channelID uint64) error
	GetQuotaUsage(userID, channelID uint64) (int, error)
	IncreaseQuotaUsage(userID, channelID uint64, delta int) (int, error)
	DecreaseQuotaUsage(userID, channelID uint64, delta int) (int, error)
	IsChannelEnabled(channelID uint64) (bool, error)
	SetChannelEnabled(channelID uint64, enabled bool) error
	IsChannelSuppressBot(channelID uint64) (bool, error)
	SetChannelSuppressBot(channelID uint64, suppressBot bool) error
	GetUser(userID uint64) (User, error)
	SetNextHintAt(userID uint64, nextHintAt time.Time) error
	GetAllRoleQuotas(channelID uint64) ([]RoleQuota, error)
	GetQuotaByRoles(channelID uint64, roleIDs []uint64) (int, error)
	ConfigureRoleQuota(channelID uint64, roleID uint64, quota int, priority int) error
	Close() error
}

type SQLiteStorage struct {
	db *sql.DB
}

var _ Storage = &SQLiteStorage{}

func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Create tables if they don't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS quota_usage (
			user_id INTEGER,
			channel_id INTEGER,
			count INTEGER DEFAULT 0,
			last_reset_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, channel_id)
		);
		CREATE TABLE IF NOT EXISTS channel_settings (
			channel_id INTEGER PRIMARY KEY,
			enabled BOOLEAN DEFAULT 0,
			suppress_bot BOOLEAN DEFAULT TRUE
		);
		CREATE TABLE IF NOT EXISTS user (
			user_id INTEGER PRIMARY KEY,
			hinted INTEGER DEFAULT 0,
			next_hint_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS role (
			role_id INTEGER,
			channel_id INTEGER,
			quota INTEGER DEFAULT 3,
			priority INTEGER DEFAULT 0,
			PRIMARY KEY (role_id, channel_id)
		);
	`)
	if err != nil {
		return nil, err
	}

	return &SQLiteStorage{db: db}, nil
}

func (s *SQLiteStorage) TryResetQuotaOnNextDay(userID, channelID uint64) error {
	taipeiTime := time.Now().UTC().Add(time.Hour * 8)
	taipeiTimeMidnight := taipeiTime.Truncate(time.Hour * 24)
	_, err := s.db.Exec(`UPDATE quota_usage SET count = 0, last_reset_at = ?
WHERE user_id = ? AND channel_id = ? AND last_reset_at <= ?`, taipeiTime, userID, channelID, taipeiTimeMidnight)
	return err
}

func (s *SQLiteStorage) ResetQuotaUsage(userID, channelID uint64) error {
	taipeiTime := time.Now().UTC().Add(time.Hour * 8)
	_, err := s.db.Exec(`INSERT INTO quota_usage (user_id, channel_id, last_reset_at) VALUES (?, ?, ?)
ON CONFLICT(user_id, channel_id) DO UPDATE SET count = 0, last_reset_at = ?`, userID, channelID, taipeiTime, taipeiTime)
	return err
}

func (s *SQLiteStorage) GetQuotaUsage(userID, channelID uint64) (int, error) {
	err := s.TryResetQuotaOnNextDay(userID, channelID)
	if err != nil {
		return 0, err
	}

	var count int
	err = s.db.QueryRow("SELECT count FROM quota_usage WHERE user_id = ? AND channel_id = ?", userID, channelID).Scan(&count)
	return count, err
}

func (s *SQLiteStorage) IncreaseQuotaUsage(userID, channelID uint64, delta int) (int, error) {
	var count int
	err := s.db.QueryRow(`
INSERT INTO quota_usage (user_id, channel_id, count)
VALUES (?, ?, 0)
ON CONFLICT(user_id, channel_id)
DO UPDATE SET count = count + ?
RETURNING count
`, userID, channelID, delta).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteStorage) DecreaseQuotaUsage(userID, channelID uint64, delta int) (int, error) {
	var count int
	err := s.db.QueryRow(`
		UPDATE quota_usage SET count = count - ? WHERE user_id = ? AND channel_id = ? AND count > 0
		RETURNING count
	`, delta, userID, channelID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *SQLiteStorage) IsChannelEnabled(channelID uint64) (bool, error) {
	var enabled bool
	err := s.db.QueryRow("SELECT enabled FROM channel_settings WHERE channel_id = ?", channelID).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return enabled, err
}

func (s *SQLiteStorage) SetChannelEnabled(channelID uint64, enabled bool) error {
	_, err := s.db.Exec(`
		INSERT INTO channel_settings (channel_id, enabled)
		VALUES (?, ?)
		ON CONFLICT(channel_id) DO UPDATE SET enabled = ?
	`, channelID, enabled, enabled)
	return err
}

func (s *SQLiteStorage) IsChannelSuppressBot(channelID uint64) (bool, error) {
	var suppressBot bool
	err := s.db.QueryRow("SELECT suppress_bot FROM channel_settings WHERE channel_id = ?", channelID).Scan(&suppressBot)
	return suppressBot, err
}

func (s *SQLiteStorage) SetChannelSuppressBot(channelID uint64, suppressBot bool) error {
	_, err := s.db.Exec(`
		INSERT INTO channel_settings (channel_id, suppress_bot)
		VALUES (?, ?)
		ON CONFLICT(channel_id) DO UPDATE SET suppress_bot = ?
	`, channelID, suppressBot, suppressBot)
	return err
}
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

func (s *SQLiteStorage) GetUser(userID uint64) (User, error) {
	var user User
	err := s.db.QueryRow("SELECT hinted, next_hint_at FROM user WHERE user_id = ?", userID).Scan(&user.Hinted, &user.NextHintAt)
	user.UserID = userID
	return user, err
}

func (s *SQLiteStorage) SetNextHintAt(userID uint64, nextHintAt time.Time) error {
	_, err := s.db.Exec(`INSERT INTO user (user_id, next_hint_at, hinted) VALUES (?, ?, 1)
	ON CONFLICT(user_id) DO UPDATE SET next_hint_at = ?, hinted = hinted + 1`, userID, nextHintAt, nextHintAt)
	return err
}

func (s *SQLiteStorage) IncreaseHinted(userID uint64) error {
	_, err := s.db.Exec(`INSERT INTO user (user_id, hinted) VALUES (?, 1)
	ON CONFLICT(user_id) DO UPDATE SET hinted = hinted + 1`, userID)
	return err
}

type RoleQuota struct {
	RoleID   uint64
	Quota    int
	Priority int
}

func (s *SQLiteStorage) GetAllRoleQuotas(channelID uint64) ([]RoleQuota, error) {
	var quotas []RoleQuota
	rows, err := s.db.Query("SELECT role_id, quota, priority FROM role WHERE channel_id = ? ORDER BY priority DESC", channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var roleID uint64
		var quota int
		var priority int
		err := rows.Scan(&roleID, &quota, &priority)
		if err != nil {
			return nil, err
		}
		quotas = append(quotas, RoleQuota{RoleID: roleID, Quota: quota, Priority: priority})
	}
	return quotas, nil
}

func (s *SQLiteStorage) GetQuotaByRoles(channelID uint64, roleIDs []uint64) (int, error) {
	sb := strings.Builder{}
	sb.WriteString("SELECT COALESCE(quota, -1) FROM role WHERE role_id IN (")
	for i, roleID := range roleIDs {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strconv.FormatUint(roleID, 10))
	}
	sb.WriteString(") AND channel_id = ? ORDER BY priority DESC LIMIT 1")
	var quota int
	err := s.db.QueryRow(sb.String(), channelID).Scan(&quota)
	return quota, err
}

func (s *SQLiteStorage) ConfigureRoleQuota(channelID uint64, roleID uint64, quota int, priority int) error {
	_, err := s.db.Exec(`INSERT INTO role (role_id, channel_id, quota, priority) VALUES (?, ?, ?, ?)
	ON CONFLICT(role_id, channel_id) DO UPDATE SET quota = ?, priority = ?`, roleID, channelID, quota, priority, quota, priority)
	return err
}
