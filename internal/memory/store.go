package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	sqlitestorage "kizuna/internal/storage/sqlite"
)

var utc8Location = time.FixedZone("UTC+8", 8*60*60)

type MessageAuthor struct {
	UserID      string
	Username    string
	GlobalName  string
	Nick        string
	DisplayName string
}

type ReplyRecord struct {
	MessageID string
	Role      string
	Content   string
	Time      time.Time
	Author    MessageAuthor
}

type ImageReference struct {
	Kind        string
	Name        string
	EmojiID     string
	URL         string
	Animated    bool
	ContentType string
}

type MessageRecord struct {
	Role    string
	GuildID string
	Content string
	Time    time.Time
	Author  MessageAuthor
	ReplyTo *ReplyRecord
	Images  []ImageReference
}

type VectorRecord struct {
	Content   string
	Rendered  string
	Embedding []float64
	Time      time.Time
}

type ChannelMemory struct {
	Summary  string
	Messages []MessageRecord
	Vectors  []VectorRecord
}

type EmbedFn func(ctx context.Context, input string) ([]float64, error)

type Store struct {
	mu      sync.Mutex
	byChID  map[string]*ChannelMemory
	embedFn EmbedFn
	db      *sql.DB
	ownsDB  bool
}

func NewStore(embedFn EmbedFn) *Store {
	db, err := sqlitestorage.OpenInMemory()
	if err != nil {
		panic(err)
	}
	store, err := NewStoreWithDB(embedFn, db)
	if err != nil {
		_ = db.Close()
		panic(err)
	}
	store.ownsDB = true
	return store
}

func NewStoreWithDB(embedFn EmbedFn, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite db is required")
	}
	store := &Store{
		byChID:  make(map[string]*ChannelMemory),
		embedFn: embedFn,
		db:      db,
	}
	if err := store.ensureSchema(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || !s.ownsDB || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensureSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS channel_summaries (
			channel_id TEXT PRIMARY KEY,
			summary TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS channel_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id TEXT NOT NULL,
			role TEXT NOT NULL,
			guild_id TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			time_unix INTEGER NOT NULL,
			author_user_id TEXT NOT NULL DEFAULT '',
			author_username TEXT NOT NULL DEFAULT '',
			author_global_name TEXT NOT NULL DEFAULT '',
			author_nick TEXT NOT NULL DEFAULT '',
			author_display_name TEXT NOT NULL DEFAULT '',
			reply_message_id TEXT NOT NULL DEFAULT '',
			reply_role TEXT NOT NULL DEFAULT '',
			reply_content TEXT NOT NULL DEFAULT '',
			reply_time_unix INTEGER NOT NULL DEFAULT 0,
			reply_author_user_id TEXT NOT NULL DEFAULT '',
			reply_author_username TEXT NOT NULL DEFAULT '',
			reply_author_global_name TEXT NOT NULL DEFAULT '',
			reply_author_nick TEXT NOT NULL DEFAULT '',
			reply_author_display_name TEXT NOT NULL DEFAULT '',
			images_json TEXT NOT NULL DEFAULT '[]'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_messages_channel_id_id
		ON channel_messages(channel_id, id)`,
		`CREATE TABLE IF NOT EXISTS channel_vectors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id TEXT NOT NULL,
			content TEXT NOT NULL,
			rendered TEXT NOT NULL,
			time_unix INTEGER NOT NULL,
			embedding_dim INTEGER NOT NULL,
			embedding BLOB NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_vectors_channel_dim_id
		ON channel_vectors(channel_id, embedding_dim, id)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AddMessage(ctx context.Context, chID, role, content string) {
	s.AddRecord(ctx, chID, MessageRecord{
		Role:    role,
		Content: content,
	})
}

func (s *Store) AddRecord(ctx context.Context, chID string, record MessageRecord) {
	record = normalizeMessageRecord(record)
	if err := s.insertMessage(chID, record); err != nil {
		log.Printf("memory add record failed: channel=%s err=%v", strings.TrimSpace(chID), err)
		return
	}
	s.mu.Lock()
	if mem := s.byChID[chID]; mem != nil {
		mem.Messages = append(mem.Messages, record)
	}
	s.mu.Unlock()
	if record.Role == "user" && record.Content != "" {
		go func() {
			indexCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			s.indexMessage(indexCtx, chID, record)
		}()
	}
}

func (s *Store) SummaryAndRecent(chID string) (summary string, messages []MessageRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mem, err := s.loadChannelLocked(chID)
	if err != nil {
		log.Printf("memory summary load failed: channel=%s err=%v", strings.TrimSpace(chID), err)
		return "", nil
	}
	return mem.Summary, append([]MessageRecord(nil), mem.Messages...)
}

func (s *Store) SetSummary(chID, summary string) {
	if err := s.upsertSummary(chID, summary); err != nil {
		log.Printf("memory set summary failed: channel=%s err=%v", strings.TrimSpace(chID), err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mem := s.byChID[chID]; mem != nil {
		mem.Summary = strings.TrimSpace(summary)
	}
}

func (s *Store) TrimHistory(chID string, keep int) {
	if err := s.trimHistoryDB(chID, keep); err != nil {
		log.Printf("memory trim history failed: channel=%s err=%v", strings.TrimSpace(chID), err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if mem := s.byChID[chID]; mem != nil && len(mem.Messages) > keep {
		mem.Messages = mem.Messages[len(mem.Messages)-keep:]
	}
}

func (s *Store) TopK(chID string, query []float64, k int) []string {
	records := s.TopKRecords(chID, query, k)
	results := make([]string, 0, len(records))
	for _, record := range records {
		results = append(results, record.Content)
	}
	return results
}

func (s *Store) TopKRecords(chID string, query []float64, k int) []VectorRecord {
	serialized, dim, err := serializeVector(query)
	if err != nil {
		log.Printf("memory serialize query vector failed: %v", err)
		return nil
	}
	rows, err := s.db.Query(`
SELECT content, rendered, time_unix
FROM channel_vectors
WHERE channel_id = ? AND embedding_dim = ?
ORDER BY vec_distance_cosine(embedding, ?)
LIMIT ?
`, strings.TrimSpace(chID), dim, serialized, k)
	if err != nil {
		log.Printf("memory vector query failed: channel=%s err=%v", strings.TrimSpace(chID), err)
		return nil
	}
	defer rows.Close()

	results := make([]VectorRecord, 0, k)
	for rows.Next() {
		var (
			content  string
			rendered string
			timeUnix int64
		)
		if err := rows.Scan(&content, &rendered, &timeUnix); err != nil {
			log.Printf("memory vector scan failed: channel=%s err=%v", strings.TrimSpace(chID), err)
			return results
		}
		results = append(results, VectorRecord{
			Content:  strings.TrimSpace(content),
			Rendered: strings.TrimSpace(rendered),
			Time:     time.Unix(timeUnix, 0).UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		log.Printf("memory vector rows failed: channel=%s err=%v", strings.TrimSpace(chID), err)
	}
	return results
}

func (s *Store) indexMessage(ctx context.Context, chID string, record MessageRecord) {
	if s.embedFn == nil {
		return
	}
	embedding, err := s.embedFn(ctx, record.Content)
	if err != nil {
		log.Printf("embedding error: %v", err)
		return
	}
	serialized, dim, err := serializeVector(embedding)
	if err != nil {
		log.Printf("embedding serialize error: %v", err)
		return
	}
	if _, err := s.db.Exec(`
INSERT INTO channel_vectors (channel_id, content, rendered, time_unix, embedding_dim, embedding)
VALUES (?, ?, ?, ?, ?, ?)
`, strings.TrimSpace(chID), record.Content, record.RenderForModel(), record.Time.UTC().Unix(), dim, serialized); err != nil {
		log.Printf("memory vector insert failed: channel=%s err=%v", strings.TrimSpace(chID), err)
		return
	}
	if _, err := s.db.Exec(`
DELETE FROM channel_vectors
WHERE channel_id = ?
AND id NOT IN (
	SELECT id
	FROM channel_vectors
	WHERE channel_id = ?
	ORDER BY id DESC
	LIMIT 200
)
`, strings.TrimSpace(chID), strings.TrimSpace(chID)); err != nil {
		log.Printf("memory vector trim failed: channel=%s err=%v", strings.TrimSpace(chID), err)
	}
}

func (s *Store) insertMessage(chID string, record MessageRecord) error {
	imagesJSON, err := json.Marshal(record.Images)
	if err != nil {
		return err
	}

	reply := normalizeReplyPointer(record.ReplyTo)
	_, err = s.db.Exec(`
INSERT INTO channel_messages (
	channel_id,
	role,
	guild_id,
	content,
	time_unix,
	author_user_id,
	author_username,
	author_global_name,
	author_nick,
	author_display_name,
	reply_message_id,
	reply_role,
	reply_content,
	reply_time_unix,
	reply_author_user_id,
	reply_author_username,
	reply_author_global_name,
	reply_author_nick,
	reply_author_display_name,
	images_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		strings.TrimSpace(chID),
		record.Role,
		record.GuildID,
		record.Content,
		record.Time.UTC().Unix(),
		record.Author.UserID,
		record.Author.Username,
		record.Author.GlobalName,
		record.Author.Nick,
		record.Author.DisplayName,
		reply.MessageID,
		reply.Role,
		reply.Content,
		reply.Time.UTC().Unix(),
		reply.Author.UserID,
		reply.Author.Username,
		reply.Author.GlobalName,
		reply.Author.Nick,
		reply.Author.DisplayName,
		string(imagesJSON),
	)
	return err
}

func (s *Store) upsertSummary(chID, summary string) error {
	_, err := s.db.Exec(`
INSERT INTO channel_summaries (channel_id, summary)
VALUES (?, ?)
ON CONFLICT(channel_id) DO UPDATE SET summary = excluded.summary
`, strings.TrimSpace(chID), strings.TrimSpace(summary))
	return err
}

func (s *Store) trimHistoryDB(chID string, keep int) error {
	chID = strings.TrimSpace(chID)
	if chID == "" {
		return nil
	}
	if keep <= 0 {
		_, err := s.db.Exec(`DELETE FROM channel_messages WHERE channel_id = ?`, chID)
		return err
	}
	_, err := s.db.Exec(`
DELETE FROM channel_messages
WHERE channel_id = ?
AND id NOT IN (
	SELECT id
	FROM channel_messages
	WHERE channel_id = ?
	ORDER BY id DESC
	LIMIT ?
)
`, chID, chID, keep)
	return err
}

func (s *Store) loadChannelLocked(chID string) (*ChannelMemory, error) {
	chID = strings.TrimSpace(chID)
	if mem := s.byChID[chID]; mem != nil {
		return mem, nil
	}

	mem := &ChannelMemory{}
	if err := s.db.QueryRow(`SELECT summary FROM channel_summaries WHERE channel_id = ?`, chID).Scan(&mem.Summary); err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	rows, err := s.db.Query(`
SELECT
	role,
	guild_id,
	content,
	time_unix,
	author_user_id,
	author_username,
	author_global_name,
	author_nick,
	author_display_name,
	reply_message_id,
	reply_role,
	reply_content,
	reply_time_unix,
	reply_author_user_id,
	reply_author_username,
	reply_author_global_name,
	reply_author_nick,
	reply_author_display_name,
	images_json
FROM channel_messages
WHERE channel_id = ?
ORDER BY id ASC
`, chID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		record, err := scanMessageRecord(rows)
		if err != nil {
			return nil, err
		}
		mem.Messages = append(mem.Messages, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.byChID[chID] = mem
	return mem, nil
}

func scanMessageRecord(scanner interface {
	Scan(dest ...any) error
}) (MessageRecord, error) {
	var (
		record         MessageRecord
		timeUnix       int64
		replyMessageID string
		replyRole      string
		replyContent   string
		replyTimeUnix  int64
		replyAuthor    MessageAuthor
		imagesJSON     string
	)
	err := scanner.Scan(
		&record.Role,
		&record.GuildID,
		&record.Content,
		&timeUnix,
		&record.Author.UserID,
		&record.Author.Username,
		&record.Author.GlobalName,
		&record.Author.Nick,
		&record.Author.DisplayName,
		&replyMessageID,
		&replyRole,
		&replyContent,
		&replyTimeUnix,
		&replyAuthor.UserID,
		&replyAuthor.Username,
		&replyAuthor.GlobalName,
		&replyAuthor.Nick,
		&replyAuthor.DisplayName,
		&imagesJSON,
	)
	if err != nil {
		return MessageRecord{}, err
	}

	record.Time = time.Unix(timeUnix, 0).UTC()
	if strings.TrimSpace(imagesJSON) != "" {
		if err := json.Unmarshal([]byte(imagesJSON), &record.Images); err != nil {
			return MessageRecord{}, err
		}
	}
	if strings.TrimSpace(replyMessageID) != "" || strings.TrimSpace(replyContent) != "" {
		record.ReplyTo = &ReplyRecord{
			MessageID: replyMessageID,
			Role:      replyRole,
			Content:   replyContent,
			Time:      time.Unix(replyTimeUnix, 0).UTC(),
			Author:    replyAuthor,
		}
	}
	return normalizeMessageRecord(record), nil
}

func normalizeReplyPointer(reply *ReplyRecord) ReplyRecord {
	if reply == nil {
		return ReplyRecord{
			Time: time.Unix(0, 0).UTC(),
		}
	}
	normalized := normalizeReplyRecord(*reply)
	return normalized
}

func serializeVector(vector []float64) ([]byte, int, error) {
	if len(vector) == 0 {
		return nil, 0, fmt.Errorf("vector is empty")
	}
	converted := make([]float32, 0, len(vector))
	for _, value := range vector {
		converted = append(converted, float32(value))
	}
	serialized, err := sqlitevec.SerializeFloat32(converted)
	if err != nil {
		return nil, 0, err
	}
	return serialized, len(converted), nil
}

func (r MessageRecord) RenderForModel() string {
	record := normalizeMessageRecord(r)
	lines := []string{
		fmt.Sprintf("时间(UTC+8): %s", record.Time.In(utc8Location).Format("2006-01-02 15:04:05")),
	}

	switch record.Role {
	case "user":
		lines = append(lines,
			"发送者ID: "+valueOrUnknown(record.Author.UserID),
			"发送者用户名: "+valueOrUnknown(record.Author.Username),
			"发送者全局名: "+valueOrUnknown(record.Author.GlobalName),
			"发送者频道昵称: "+valueOrUnknown(record.Author.Nick),
			"发送者显示名: "+valueOrUnknown(record.Author.DisplayName),
		)
	case "assistant":
		lines = append(lines, "发送者: 机器人")
	default:
		lines = append(lines, "发送者角色: "+valueOrUnknown(record.Role))
	}

	lines = append(lines, "内容:", record.Content)
	if len(record.Images) > 0 {
		lines = append(lines, "", "附带图片/表情:")
		for index, image := range record.Images {
			lines = append(lines, fmt.Sprintf("%d. %s", index+1, renderImageReference(image)))
		}
	}
	if record.ReplyTo != nil {
		reply := normalizeReplyRecord(*record.ReplyTo)
		lines = append(lines,
			"",
			"这条消息是在回复以下消息:",
			"被回复消息ID: "+valueOrUnknown(reply.MessageID),
			fmt.Sprintf("被回复消息时间(UTC+8): %s", reply.Time.In(utc8Location).Format("2006-01-02 15:04:05")),
			"被回复消息角色: "+valueOrUnknown(reply.Role),
			"被回复发送者ID: "+valueOrUnknown(reply.Author.UserID),
			"被回复发送者用户名: "+valueOrUnknown(reply.Author.Username),
			"被回复发送者全局名: "+valueOrUnknown(reply.Author.GlobalName),
			"被回复发送者频道昵称: "+valueOrUnknown(reply.Author.Nick),
			"被回复发送者显示名: "+valueOrUnknown(reply.Author.DisplayName),
			"被回复消息内容:",
			reply.Content,
		)
	}
	return strings.Join(lines, "\n")
}

func normalizeMessageRecord(record MessageRecord) MessageRecord {
	record.Role = strings.TrimSpace(record.Role)
	record.GuildID = strings.TrimSpace(record.GuildID)
	record.Content = strings.TrimSpace(record.Content)
	if record.Time.IsZero() {
		record.Time = time.Now()
	}
	record.Author = normalizeAuthor(record.Author)
	if record.ReplyTo != nil {
		reply := normalizeReplyRecord(*record.ReplyTo)
		record.ReplyTo = &reply
	}
	record.Images = normalizeImageReferences(record.Images)
	return record
}

func normalizeReplyRecord(reply ReplyRecord) ReplyRecord {
	reply.MessageID = strings.TrimSpace(reply.MessageID)
	reply.Role = strings.TrimSpace(reply.Role)
	reply.Content = strings.TrimSpace(reply.Content)
	if reply.Role == "" {
		reply.Role = "unknown"
	}
	if reply.Time.IsZero() {
		reply.Time = time.Now()
	}
	reply.Author = normalizeAuthor(reply.Author)
	return reply
}

func normalizeAuthor(author MessageAuthor) MessageAuthor {
	author.UserID = strings.TrimSpace(author.UserID)
	author.Username = strings.TrimSpace(author.Username)
	author.GlobalName = strings.TrimSpace(author.GlobalName)
	author.Nick = strings.TrimSpace(author.Nick)
	author.DisplayName = strings.TrimSpace(author.DisplayName)
	if author.DisplayName == "" {
		switch {
		case author.Nick != "":
			author.DisplayName = author.Nick
		case author.GlobalName != "":
			author.DisplayName = author.GlobalName
		case author.Username != "":
			author.DisplayName = author.Username
		case author.UserID != "":
			author.DisplayName = author.UserID
		}
	}
	return author
}

func valueOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未设置"
	}
	return value
}

func normalizeImageReferences(images []ImageReference) []ImageReference {
	if len(images) == 0 {
		return nil
	}

	normalized := make([]ImageReference, 0, len(images))
	seen := make(map[string]struct{}, len(images))
	for _, image := range images {
		image.Kind = strings.TrimSpace(image.Kind)
		image.Name = strings.TrimSpace(image.Name)
		image.EmojiID = strings.TrimSpace(image.EmojiID)
		image.URL = strings.TrimSpace(image.URL)
		image.ContentType = strings.TrimSpace(image.ContentType)
		if image.URL == "" {
			continue
		}
		key := image.Kind + "\x00" + image.EmojiID + "\x00" + image.URL
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, image)
	}
	return normalized
}

func renderImageReference(image ImageReference) string {
	url := valueOrUnknown(image.URL)
	switch image.Kind {
	case "custom_emoji":
		tag := valueOrUnknown(image.Name)
		if strings.TrimSpace(image.EmojiID) != "" && strings.TrimSpace(image.Name) != "" {
			if image.Animated {
				tag = fmt.Sprintf("<a:%s:%s>", image.Name, image.EmojiID)
			} else {
				tag = fmt.Sprintf("<:%s:%s>", image.Name, image.EmojiID)
			}
		}
		return fmt.Sprintf("自定义表情 %s, 图片URL: %s", tag, url)
	case "attachment":
		label := valueOrUnknown(image.Name)
		if strings.TrimSpace(image.ContentType) != "" {
			label += " (" + image.ContentType + ")"
		}
		return fmt.Sprintf("图片附件 %s, 图片URL: %s", label, url)
	default:
		return fmt.Sprintf("图片资源 %s, 图片URL: %s", valueOrUnknown(image.Name), url)
	}
}
