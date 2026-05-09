package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"agentswitcher/internal/agent"
)

const (
	compactionInterval = 12
	maxRecentMessages  = 24
)

type Repository struct {
	db *sql.DB
}

type Session struct {
	ID                 string
	Agent              agent.Kind
	Title              string
	Summary            string
	UserPromptCount    int
	CompactedPromptCnt int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Message struct {
	ID        int64
	SessionID string
	Role      string
	Content   string
	CreatedAt time.Time
}

type Standard struct {
	ID        int64
	SessionID string
	Path      string
	CreatedAt time.Time
}

type ContextSnapshot struct {
	Session        Session
	Standards      []Standard
	RecentMessages []Message
}

func NewRepository(path string) (*Repository, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if _, err := db.Exec(`
		PRAGMA foreign_keys = ON;
		PRAGMA journal_mode = WAL;

		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			agent TEXT NOT NULL,
			title TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			user_prompt_count INTEGER NOT NULL DEFAULT 0,
			compacted_prompt_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_sessions_agent_updated_at
			ON sessions(agent, updated_at DESC);

		CREATE INDEX IF NOT EXISTS idx_messages_session_id_id
			ON messages(session_id, id);

		CREATE TABLE IF NOT EXISTS session_standards (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			path TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(session_id, path),
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_session_standards_session_id
			ON session_standards(session_id, id);
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize sqlite schema: %w", err)
	}

	return &Repository{db: db}, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) CreateSession(ctx context.Context, kind agent.Kind) (Session, error) {
	now := time.Now().UTC()
	session := Session{
		ID:        newUUID(),
		Agent:     kind,
		Title:     defaultTitle(kind),
		CreatedAt: now,
		UpdatedAt: now,
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO sessions (
			id, agent, title, summary, user_prompt_count, compacted_prompt_count, created_at, updated_at
		) VALUES (?, ?, ?, '', 0, 0, ?, ?)`,
		session.ID,
		string(session.Agent),
		session.Title,
		session.CreatedAt.Format(time.RFC3339Nano),
		session.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}

	return session, nil
}

func (r *Repository) ListSessions(ctx context.Context, kind agent.Kind, limit int) ([]Session, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, agent, title, summary, user_prompt_count, compacted_prompt_count, created_at, updated_at
		FROM sessions
		WHERE agent = ?
		ORDER BY updated_at DESC
		LIMIT ?`,
		string(kind),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}

	return sessions, nil
}

func (r *Repository) ListAllSessions(ctx context.Context, limit int) ([]Session, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, agent, title, summary, user_prompt_count, compacted_prompt_count, created_at, updated_at
		FROM sessions
		ORDER BY updated_at DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list all sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}

	return sessions, nil
}

func (r *Repository) UpdateSessionAgent(ctx context.Context, sessionID string, kind agent.Kind) (Session, error) {
	if _, err := r.db.ExecContext(
		ctx,
		`UPDATE sessions SET agent = ?, updated_at = ? WHERE id = ?`,
		string(kind),
		time.Now().UTC().Format(time.RFC3339Nano),
		sessionID,
	); err != nil {
		return Session{}, fmt.Errorf("update session agent: %w", err)
	}
	return r.GetSession(ctx, sessionID)
}

func (r *Repository) GetSession(ctx context.Context, id string) (Session, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, agent, title, summary, user_prompt_count, compacted_prompt_count, created_at, updated_at
		FROM sessions
		WHERE id = ?`,
		id,
	)
	session, err := scanSession(row)
	if err != nil {
		return Session{}, fmt.Errorf("get session %s: %w", id, err)
	}
	return session, nil
}

func (r *Repository) GetContextSnapshot(ctx context.Context, sessionID string) (ContextSnapshot, error) {
	session, err := r.GetSession(ctx, sessionID)
	if err != nil {
		return ContextSnapshot{}, err
	}

	standards, err := r.ListStandards(ctx, sessionID)
	if err != nil {
		return ContextSnapshot{}, err
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, session_id, role, content, created_at
		FROM messages
		WHERE session_id = ?
		ORDER BY id DESC
		LIMIT ?`,
		sessionID,
		maxRecentMessages,
	)
	if err != nil {
		return ContextSnapshot{}, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var reversed []Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return ContextSnapshot{}, err
		}
		reversed = append(reversed, message)
	}
	if err := rows.Err(); err != nil {
		return ContextSnapshot{}, fmt.Errorf("iterate messages: %w", err)
	}

	messages := make([]Message, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		messages = append(messages, reversed[i])
	}

	return ContextSnapshot{
		Session:        session,
		Standards:      standards,
		RecentMessages: messages,
	}, nil
}

func (r *Repository) ListStandards(ctx context.Context, sessionID string) ([]Standard, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, session_id, path, created_at
		FROM session_standards
		WHERE session_id = ?
		ORDER BY path ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list standards: %w", err)
	}
	defer rows.Close()

	var standards []Standard
	for rows.Next() {
		standard, err := scanStandard(rows)
		if err != nil {
			return nil, err
		}
		standards = append(standards, standard)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate standards: %w", err)
	}

	return standards, nil
}

func (r *Repository) ReplaceStandards(ctx context.Context, sessionID string, paths []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin standards transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM session_standards WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("clear standards: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, path := range paths {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO session_standards (session_id, path, created_at) VALUES (?, ?, ?)`,
			sessionID,
			strings.TrimSpace(path),
			now,
		); err != nil {
			return fmt.Errorf("insert standard %s: %w", path, err)
		}
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE sessions SET updated_at = ? WHERE id = ?`,
		now,
		sessionID,
	); err != nil {
		return fmt.Errorf("touch session after standards update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit standards transaction: %w", err)
	}

	return nil
}

func (r *Repository) ListMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, session_id, role, content, created_at
		FROM messages
		WHERE session_id = ?
		ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session messages: %w", err)
	}

	return messages, nil
}

func (r *Repository) AddExchange(ctx context.Context, sessionID, userPrompt, assistantReply string) (Session, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO messages (session_id, role, content, created_at) VALUES (?, 'user', ?, ?)`,
		sessionID,
		strings.TrimSpace(userPrompt),
		now.Format(time.RFC3339Nano),
	); err != nil {
		return Session{}, fmt.Errorf("insert user message: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO messages (session_id, role, content, created_at) VALUES (?, 'assistant', ?, ?)`,
		sessionID,
		strings.TrimSpace(assistantReply),
		now.Format(time.RFC3339Nano),
	); err != nil {
		return Session{}, fmt.Errorf("insert assistant message: %w", err)
	}

	current, err := querySessionTx(ctx, tx, sessionID)
	if err != nil {
		return Session{}, err
	}

	title := current.Title
	if current.UserPromptCount == 0 {
		title = makeTitle(userPrompt, current.Agent)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE sessions
		SET title = ?, user_prompt_count = user_prompt_count + 1, updated_at = ?
		WHERE id = ?`,
		title,
		now.Format(time.RFC3339Nano),
		sessionID,
	); err != nil {
		return Session{}, fmt.Errorf("update session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit transaction: %w", err)
	}

	return r.GetSession(ctx, sessionID)
}

func (r *Repository) SaveCompaction(ctx context.Context, sessionID, summary string, compactedPromptCount int) (Session, error) {
	if _, err := r.db.ExecContext(
		ctx,
		`UPDATE sessions
		SET summary = ?, compacted_prompt_count = ?, updated_at = ?
		WHERE id = ?`,
		strings.TrimSpace(summary),
		compactedPromptCount,
		time.Now().UTC().Format(time.RFC3339Nano),
		sessionID,
	); err != nil {
		return Session{}, fmt.Errorf("save compaction: %w", err)
	}
	return r.GetSession(ctx, sessionID)
}

func (r *Repository) NeedCompaction(session Session) bool {
	return session.UserPromptCount-session.CompactedPromptCnt >= compactionInterval
}

func (r *Repository) GetMessagesForCompaction(ctx context.Context, sessionID string) ([]Message, error) {
	session, err := r.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, session_id, role, content, created_at
		FROM messages
		WHERE session_id = ?
		ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("load compaction messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	userPromptsSeen := 0
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		if message.Role == "user" {
			userPromptsSeen++
		}
		if userPromptsSeen <= session.CompactedPromptCnt {
			continue
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate compaction messages: %w", err)
	}

	return messages, nil
}

func scanSession(scanner interface{ Scan(dest ...any) error }) (Session, error) {
	var session Session
	var createdAt string
	var updatedAt string
	var agentName string

	if err := scanner.Scan(
		&session.ID,
		&agentName,
		&session.Title,
		&session.Summary,
		&session.UserPromptCount,
		&session.CompactedPromptCnt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Session{}, err
	}

	session.Agent = agent.Kind(agentName)
	session.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	session.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	return session, nil
}

func scanMessage(scanner interface{ Scan(dest ...any) error }) (Message, error) {
	var message Message
	var createdAt string
	if err := scanner.Scan(
		&message.ID,
		&message.SessionID,
		&message.Role,
		&message.Content,
		&createdAt,
	); err != nil {
		return Message{}, err
	}
	message.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return message, nil
}

func scanStandard(scanner interface{ Scan(dest ...any) error }) (Standard, error) {
	var standard Standard
	var createdAt string
	if err := scanner.Scan(
		&standard.ID,
		&standard.SessionID,
		&standard.Path,
		&createdAt,
	); err != nil {
		return Standard{}, err
	}
	standard.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return standard, nil
}

func querySessionTx(ctx context.Context, tx *sql.Tx, sessionID string) (Session, error) {
	row := tx.QueryRowContext(
		ctx,
		`SELECT id, agent, title, summary, user_prompt_count, compacted_prompt_count, created_at, updated_at
		FROM sessions WHERE id = ?`,
		sessionID,
	)
	session, err := scanSession(row)
	if err != nil {
		return Session{}, fmt.Errorf("query session in transaction: %w", err)
	}
	return session, nil
}

func defaultTitle(kind agent.Kind) string {
	def, ok := agent.Find(kind)
	if !ok {
		return "New session"
	}
	return fmt.Sprintf("New %s session", def.Label)
}

func makeTitle(prompt string, kind agent.Kind) string {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return defaultTitle(kind)
	}
	if len(trimmed) > 48 {
		return trimmed[:48] + "..."
	}
	return trimmed
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}
