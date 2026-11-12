package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"asimi/internal/ai"
	"asimi/internal/configuration"
	_ "golang.org/x/image/webp"
)

const maxImageBytes = configuration.MaxImageMiB << 20
const maxImagePixels = 25_000_000

func (s *Service) attachmentDir() string { return filepath.Join(s.dataDir, "attachments") }

// Attachment paths are generated solely from random IDs; no selected paths cross this boundary.
func (s *Service) attachmentPath(id string) (string, error) {
	if len(id) != 32 {
		return "", codedError("INVALID_ATTACHMENT")
	}
	if _, e := hex.DecodeString(id); e != nil {
		return "", codedError("INVALID_ATTACHMENT")
	}
	return filepath.Join(s.attachmentDir(), id+".image"), nil
}
func (s *Service) importAttachmentLocked(name, encoded string) (ai.Attachment, error) {
	var a ai.Attachment
	config, err := s.resolvedConfiguration()
	if err != nil {
		return a, err
	}
	limit := config.Media.ImageMiB << 20
	if len(encoded) > base64.StdEncoding.EncodedLen(limit) {
		return a, codedError("IMAGE_TOO_LARGE")
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil || len(raw) == 0 {
		return a, codedError("INVALID_IMAGE")
	}
	if len(raw) > limit {
		return a, codedError("IMAGE_TOO_LARGE")
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return a, codedError("INVALID_IMAGE")
	}
	if format != "png" && format != "jpeg" && format != "gif" && format != "webp" {
		return a, codedError("UNSUPPORTED_IMAGE_FORMAT")
	}
	if format == "gif" {
		if !singleFrameGIF(raw) {
			return a, codedError("ANIMATED_IMAGE_UNSUPPORTED")
		}
	}
	im, decoded, e := image.Decode(bytes.NewReader(raw))
	if e != nil || decoded != format {
		return a, codedError("INVALID_IMAGE")
	}
	var safe bytes.Buffer
	mime, ext := "image/png", ".png"
	if format == "jpeg" {
		mime, ext = "image/jpeg", ".jpg"
		e = jpeg.Encode(&safe, im, &jpeg.Options{Quality: 90})
	} else {
		e = png.Encode(&safe, im)
	}
	if e != nil || safe.Len() > limit {
		return a, codedError("IMAGE_TOO_LARGE")
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		name = "image"
	}
	if len(name) > 120 {
		name = "image"
	}
	name = strings.TrimSuffix(name, filepath.Ext(name)) + ext
	sum := sha256.Sum256(safe.Bytes())
	a = ai.Attachment{ID: newID(), Name: name, MIME: mime, Bytes: int64(safe.Len()), Width: cfg.Width, Height: cfg.Height, SHA256: hex.EncodeToString(sum[:])}
	if e = os.MkdirAll(s.attachmentDir(), 0700); e != nil {
		return ai.Attachment{}, e
	}
	path, _ := s.attachmentPath(a.ID)
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return ai.Attachment{}, e
	}
	_, writeErr := f.Write(safe.Bytes())
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return ai.Attachment{}, codedError("STORAGE_IO")
	}
	_, e = s.db.Exec("INSERT INTO ai_attachments(id,json,created_at) VALUES(?,?,?)", a.ID, encode(a), now())
	if e != nil {
		_ = os.Remove(path)
		return ai.Attachment{}, e
	}
	a.PreviewDataURL = "data:" + a.MIME + ";base64," + base64.StdEncoding.EncodeToString(safe.Bytes())
	return a, nil
}
func (s *Service) attachmentLocked(id string, preview bool) (ai.Attachment, error) {
	var a ai.Attachment
	var raw string
	path, e := s.attachmentPath(id)
	if e != nil {
		return a, e
	}
	if e = s.db.QueryRow("SELECT json FROM ai_attachments WHERE id=?", id).Scan(&raw); e != nil {
		return a, e
	}
	if e = json.Unmarshal([]byte(raw), &a); e != nil {
		return a, e
	}
	if preview {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > maxImageBytes {
			return a, codedError("ATTACHMENT_UNAVAILABLE")
		}
		f, e := openAttachmentFile(path)
		if e != nil {
			return a, codedError("ATTACHMENT_UNAVAILABLE")
		}
		data, e := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
		closeErr := f.Close()
		if e != nil {
			return a, codedError("ATTACHMENT_UNAVAILABLE")
		}
		if closeErr != nil || len(data) > maxImageBytes || int64(len(data)) != a.Bytes {
			return a, codedError("ATTACHMENT_UNAVAILABLE")
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != a.SHA256 {
			return a, codedError("ATTACHMENT_UNAVAILABLE")
		}
		a.PreviewDataURL = "data:" + a.MIME + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return a, nil
}

// Scan structural blocks before decoding any pixels, rejecting a second frame
// without allocating its LZW output. Extensions are bounded by the input slice.
func singleFrameGIF(raw []byte) bool {
	if len(raw) < 13 || string(raw[:3]) != "GIF" {
		return false
	}
	pos := 13
	if raw[10]&0x80 != 0 {
		pos += 3 * (1 << ((raw[10] & 7) + 1))
	}
	blocks := func() bool {
		for {
			if pos >= len(raw) {
				return false
			}
			n := int(raw[pos])
			pos++
			if n == 0 {
				return true
			}
			if n > len(raw)-pos {
				return false
			}
			pos += n
		}
	}
	frames := 0
	for pos < len(raw) {
		kind := raw[pos]
		pos++
		switch kind {
		case 0x3b:
			return frames == 1 && pos == len(raw)
		case 0x21:
			if pos >= len(raw) {
				return false
			}
			pos++
			if !blocks() {
				return false
			}
		case 0x2c:
			frames++
			if frames > 1 || len(raw)-pos < 9 {
				return false
			}
			packed := raw[pos+8]
			pos += 9
			if packed&0x80 != 0 {
				pos += 3 * (1 << ((packed & 7) + 1))
			}
			if pos >= len(raw) {
				return false
			}
			pos++
			if !blocks() {
				return false
			}
		default:
			return false
		}
	}
	return false
}
func (s *Service) deleteAttachmentLocked(id string) (bool, error) {
	s.attachmentMu.Lock()
	defer s.attachmentMu.Unlock()
	path, e := s.attachmentPath(id)
	if e != nil {
		return false, e
	}
	var n int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM ai_message_attachments WHERE attachment_id=?", id).Scan(&n); e != nil {
		return false, e
	}
	if n > 0 {
		return false, codedError("ATTACHMENT_IN_USE")
	}
	var exists int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM ai_attachments WHERE id=?", id).Scan(&exists); e != nil {
		return false, e
	}
	if exists == 0 {
		if e = s.removeAttachmentFile(path + ".deleting"); e != nil && !os.IsNotExist(e) {
			return false, codedError("ATTACHMENT_RECOVERY_REQUIRED")
		}
		if _, e = s.db.Exec("DELETE FROM ai_attachment_cleanup WHERE attachment_id=?", id); e != nil {
			return false, e
		}
		return false, nil
	}
	if _, e = s.db.Exec("INSERT OR IGNORE INTO ai_attachment_cleanup(attachment_id,conversation_id,created_at) VALUES(?,NULL,?)", id, now()); e != nil {
		return false, e
	}
	// Rename first, retaining recoverable bytes until SQLite accepts deletion.
	// Startup reconciles a crash on either side of the metadata commit.
	pending := path + ".deleting"
	moved := false
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return false, codedError("ATTACHMENT_UNAVAILABLE")
		}
		if e = os.Rename(path, pending); e != nil {
			return false, codedError("STORAGE_IO")
		}
		moved = true
	} else if !os.IsNotExist(statErr) {
		return false, codedError("STORAGE_IO")
	}
	r, e := s.db.Exec("DELETE FROM ai_attachments WHERE id=?", id)
	if e != nil {
		if moved {
			if restoreErr := os.Rename(pending, path); restoreErr != nil {
				return false, codedError("ATTACHMENT_RECOVERY_REQUIRED")
			}
		}
		return false, e
	}
	if moved {
		if e = s.removeAttachmentFile(pending); e != nil {
			return false, codedError("ATTACHMENT_RECOVERY_REQUIRED")
		}
	}
	if _, e = s.db.Exec("DELETE FROM ai_attachment_cleanup WHERE attachment_id=?", id); e != nil {
		return false, e
	}
	n64, e := r.RowsAffected()
	return n64 > 0, e
}

func (s *Service) removeAttachmentFile(path string) error {
	if s.removeAttachment != nil {
		return s.removeAttachment(path)
	}
	return os.Remove(path)
}

// Reconcile only app-generated file names; never traverse user paths or folders.
func (s *Service) reconcileAttachments() error {
	entries, e := os.ReadDir(s.attachmentDir())
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return codedError("STORAGE_IO")
	}
	for _, entry := range entries {
		name := entry.Name()
		pending := strings.HasSuffix(name, ".image.deleting")
		suffix := ".image"
		if pending {
			suffix = ".image.deleting"
		}
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		id := strings.TrimSuffix(name, suffix)
		path, e := s.attachmentPath(id)
		if e != nil {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			return codedError("STORAGE_IO")
		}
		if !info.Mode().IsRegular() {
			return codedError("ATTACHMENT_UNAVAILABLE")
		}
		var n int
		if e = s.db.QueryRow("SELECT COUNT(*) FROM ai_attachments WHERE id=?", id).Scan(&n); e != nil {
			return e
		}
		if pending {
			if n > 0 {
				if _, e = os.Lstat(path); os.IsNotExist(e) {
					if e = os.Rename(path+".deleting", path); e != nil {
						return codedError("ATTACHMENT_RECOVERY_REQUIRED")
					}
				} else if e != nil {
					return codedError("STORAGE_IO")
				} else {
					return codedError("ATTACHMENT_RECOVERY_REQUIRED")
				}
			} else if e = os.Remove(path + ".deleting"); e != nil {
				return codedError("ATTACHMENT_RECOVERY_REQUIRED")
			}
		} else if n == 0 {
			if e = os.Remove(path); e != nil {
				return codedError("ATTACHMENT_RECOVERY_REQUIRED")
			}
		}
	}
	return nil
}

func (s *Service) retryAttachmentCleanup(conversationID string) error {
	query := "SELECT attachment_id FROM ai_attachment_cleanup"
	args := []any{}
	if conversationID != "" {
		query += " WHERE conversation_id=?"
		args = append(args, conversationID)
	}
	rows, e := s.db.Query(query, args...)
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		var n int
		if e = s.db.QueryRow("SELECT COUNT(*) FROM ai_message_attachments WHERE attachment_id=?", id).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			if _, e = s.db.Exec("DELETE FROM ai_attachment_cleanup WHERE attachment_id=?", id); e != nil {
				return e
			}
			continue
		}
		if _, e = s.deleteAttachmentLocked(id); e != nil {
			return e
		}
	}
	return nil
}
