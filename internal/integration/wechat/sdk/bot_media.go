package wechatbot

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nextsko/mocode-agent/internal/integration/wechat/sdk/internal/auth"
	"github.com/nextsko/mocode-agent/internal/integration/wechat/sdk/internal/crypto"
	"github.com/nextsko/mocode-agent/internal/integration/wechat/sdk/internal/protocol"
)

func (b *Bot) Reply(ctx context.Context, msg *IncomingMessage, text string) error {
	b.contextTokens.Store(msg.UserID, msg.ContextToken)
	return b.sendText(ctx, msg.UserID, text, msg.ContextToken)
}

// Send sends a text message to a user (requires prior context_token).

func (b *Bot) Send(ctx context.Context, userID, text string) error {
	ct, ok := b.contextTokens.Load(userID)
	if !ok {
		return fmt.Errorf("no context_token for user %s", userID)
	}
	return b.sendText(ctx, userID, text, ct.(string))
}

// SendTyping shows the "typing..." indicator.

func (b *Bot) SendTyping(ctx context.Context, userID string) error {
	ct, ok := b.contextTokens.Load(userID)
	if !ok {
		return fmt.Errorf("no context_token for user %s", userID)
	}
	creds := b.getCreds()
	config, err := b.client.GetConfig(ctx, creds.BaseURL, creds.Token, userID, ct.(string))
	if err != nil {
		return err
	}
	if config.TypingTicket == "" {
		return nil
	}
	return b.client.SendTyping(ctx, creds.BaseURL, creds.Token, userID, config.TypingTicket, 1)
}

// StopTyping cancels the "typing..." indicator.

func (b *Bot) StopTyping(ctx context.Context, userID string) error {
	ct, ok := b.contextTokens.Load(userID)
	if !ok {
		return nil
	}
	creds := b.getCreds()
	config, err := b.client.GetConfig(ctx, creds.BaseURL, creds.Token, userID, ct.(string))
	if err != nil {
		return err
	}
	if config.TypingTicket == "" {
		return nil
	}
	return b.client.SendTyping(ctx, creds.BaseURL, creds.Token, userID, config.TypingTicket, 2)
}

// SendContent describes what to send. Use one of:
//   - SendText("Hello!")
//   - SendImage(data)
//   - SendVideo(data)
//   - SendFile(data, "report.pdf")

func SendText(text string) SendContent { return SendContent{Text: text} }

// SendImage creates an image SendContent.

func SendImage(data []byte) SendContent { return SendContent{Image: data} }

// SendVideo creates a video SendContent.

func SendVideo(data []byte) SendContent { return SendContent{Video: data} }

// SendFile creates a file SendContent.

func SendFile(data []byte, fileName string) SendContent {
	return SendContent{File: data, FileName: fileName}
}

// ReplyContent replies with any content type.

func (b *Bot) ReplyContent(ctx context.Context, msg *IncomingMessage, content SendContent) error {
	b.contextTokens.Store(msg.UserID, msg.ContextToken)
	return b.sendContent(ctx, msg.UserID, msg.ContextToken, content)
}

// SendMedia sends any content type to a user.

func (b *Bot) SendMedia(ctx context.Context, userID string, content SendContent) error {
	ct, ok := b.contextTokens.Load(userID)
	if !ok {
		return fmt.Errorf("no context_token for user %s", userID)
	}
	return b.sendContent(ctx, userID, ct.(string), content)
}

// Download downloads media from an incoming message.
// Returns nil if the message has no media. Priority: image > file > video > voice.

func (b *Bot) Download(ctx context.Context, msg *IncomingMessage) (*DownloadedMedia, error) {
	if len(msg.Images) > 0 && msg.Images[0].Media != nil {
		data, err := b.cdnDownload(ctx, msg.Images[0].Media, msg.Images[0].AESKey)
		if err != nil {
			return nil, err
		}
		return &DownloadedMedia{Data: data, Type: "image"}, nil
	}

	if len(msg.Files) > 0 && msg.Files[0].Media != nil {
		data, err := b.cdnDownload(ctx, msg.Files[0].Media, "")
		if err != nil {
			return nil, err
		}
		name := msg.Files[0].FileName
		if name == "" {
			name = "file.bin"
		}
		return &DownloadedMedia{Data: data, Type: "file", FileName: name}, nil
	}

	if len(msg.Videos) > 0 && msg.Videos[0].Media != nil {
		data, err := b.cdnDownload(ctx, msg.Videos[0].Media, "")
		if err != nil {
			return nil, err
		}
		return &DownloadedMedia{Data: data, Type: "video"}, nil
	}

	if len(msg.Voices) > 0 && msg.Voices[0].Media != nil {
		data, err := b.cdnDownload(ctx, msg.Voices[0].Media, "")
		if err != nil {
			return nil, err
		}
		return &DownloadedMedia{Data: data, Type: "voice", Format: "silk"}, nil
	}

	return nil, nil
}

// DownloadRaw downloads and decrypts a raw CDN media reference.

func (b *Bot) DownloadRaw(ctx context.Context, media *CDNMedia, aeskeyOverride string) ([]byte, error) {
	return b.cdnDownload(ctx, media, aeskeyOverride)
}

// Upload uploads data to WeChat CDN without sending a message.

func (b *Bot) Upload(ctx context.Context, data []byte, userID string, mediaType int) (*UploadResult, error) {
	creds := b.getCreds()
	if creds == nil {
		return nil, fmt.Errorf("not logged in; call Login() first")
	}
	return b.cdnUpload(ctx, creds, data, userID, mediaType)
}

// Run starts the long-poll loop. Blocks until Stop() is called or context is cancelled.

func (b *Bot) sendContent(ctx context.Context, userID, contextToken string, content SendContent) error {
	// Text
	if content.Text != "" {
		return b.sendText(ctx, userID, content.Text, contextToken)
	}

	creds := b.getCreds()
	if creds == nil {
		return fmt.Errorf("not logged in; call Login() first")
	}

	// Image
	if content.Image != nil {
		result, err := b.cdnUpload(ctx, creds, content.Image, userID, int(MediaImage))
		if err != nil {
			return err
		}
		items := []map[string]interface{}{}
		if content.Caption != "" {
			items = append(items, map[string]interface{}{
				"type": 1, "text_item": map[string]string{"text": content.Caption},
			})
		}
		items = append(items, map[string]interface{}{
			"type": 2, "image_item": map[string]interface{}{
				"media":    cdnMediaMap(&result.Media),
				"mid_size": result.EncryptedFileSize,
			},
		})
		msg := protocol.BuildMediaMessage(userID, contextToken, items)
		return b.client.SendMessage(ctx, creds.BaseURL, creds.Token, msg)
	}

	// Video
	if content.Video != nil {
		result, err := b.cdnUpload(ctx, creds, content.Video, userID, int(MediaVideo))
		if err != nil {
			return err
		}
		items := []map[string]interface{}{}
		if content.Caption != "" {
			items = append(items, map[string]interface{}{
				"type": 1, "text_item": map[string]string{"text": content.Caption},
			})
		}
		items = append(items, map[string]interface{}{
			"type": 5, "video_item": map[string]interface{}{
				"media":      cdnMediaMap(&result.Media),
				"video_size": result.EncryptedFileSize,
			},
		})
		msg := protocol.BuildMediaMessage(userID, contextToken, items)
		return b.client.SendMessage(ctx, creds.BaseURL, creds.Token, msg)
	}

	// File (auto-route by extension)
	if content.File != nil {
		fileName := content.FileName
		if fileName == "" {
			fileName = "file.bin"
		}
		cat := categorizeByExtension(fileName)
		if cat == "image" {
			return b.sendContent(ctx, userID, contextToken, SendContent{Image: content.File, Caption: content.Caption})
		}
		if cat == "video" {
			return b.sendContent(ctx, userID, contextToken, SendContent{Video: content.File, Caption: content.Caption})
		}
		// Generic file
		if content.Caption != "" {
			_ = b.sendText(ctx, userID, content.Caption, contextToken)
		}
		result, err := b.cdnUpload(ctx, creds, content.File, userID, int(MediaFile))
		if err != nil {
			return err
		}
		items := []map[string]interface{}{
			{"type": 4, "file_item": map[string]interface{}{
				"media":     cdnMediaMap(&result.Media),
				"file_name": fileName,
				"len":       strconv.Itoa(len(content.File)),
			}},
		}
		msg := protocol.BuildMediaMessage(userID, contextToken, items)
		return b.client.SendMessage(ctx, creds.BaseURL, creds.Token, msg)
	}

	return fmt.Errorf("empty SendContent")
}

func (b *Bot) cdnDownload(ctx context.Context, media *CDNMedia, aeskeyOverride string) ([]byte, error) {
	downloadURL := fmt.Sprintf("%s/download?encrypted_query_param=%s",
		protocol.CDNBaseURL, url.QueryEscape(media.EncryptQueryParam))

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cdn download request: %w", err)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cdn download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cdn download failed: HTTP %d", resp.StatusCode)
	}

	ciphertext, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cdn download read: %w", err)
	}

	keySource := aeskeyOverride
	if keySource == "" {
		keySource = media.AESKey
	}
	if keySource == "" {
		return nil, fmt.Errorf("no AES key available for decryption")
	}

	aesKey, err := crypto.DecodeAESKey(keySource)
	if err != nil {
		return nil, fmt.Errorf("decode aes key: %w", err)
	}

	return crypto.DecryptAESECB(ciphertext, aesKey)
}

func (b *Bot) cdnUpload(ctx context.Context, creds *auth.Credentials, data []byte, userID string, mediaType int) (*UploadResult, error) {
	aesKey, err := crypto.GenerateAESKey()
	if err != nil {
		return nil, fmt.Errorf("generate aes key: %w", err)
	}
	ciphertext, err := crypto.EncryptAESECB(data, aesKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	var fileKeyBuf [16]byte
	if _, err := rand.Read(fileKeyBuf[:]); err != nil {
		return nil, fmt.Errorf("generate file key: %w", err)
	}
	fileKey := hex.EncodeToString(fileKeyBuf[:])

	rawMD5 := md5.Sum(data)
	rawMD5Hex := hex.EncodeToString(rawMD5[:])

	uploadResp, err := b.client.GetUploadURL(ctx, creds.BaseURL, creds.Token, protocol.GetUploadURLRequest{
		FileKey:     fileKey,
		MediaType:   mediaType,
		ToUserID:    userID,
		RawSize:     len(data),
		RawFileMD5:  rawMD5Hex,
		FileSize:    len(ciphertext),
		NoNeedThumb: true,
		AESKey:      crypto.EncodeAESKeyHex(aesKey),
	})
	if err != nil {
		return nil, fmt.Errorf("getuploadurl: %w", err)
	}
	if uploadResp.UploadParam == "" {
		return nil, fmt.Errorf("getuploadurl did not return upload_param")
	}

	uploadURL := fmt.Sprintf("%s/upload?encrypted_query_param=%s&filekey=%s",
		protocol.CDNBaseURL,
		url.QueryEscape(uploadResp.UploadParam),
		url.QueryEscape(fileKey))

	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, bytes.NewReader(ciphertext))
	if err != nil {
		return nil, fmt.Errorf("cdn upload request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cdn upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errMsg := resp.Header.Get("x-error-message")
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("cdn upload failed: %s", errMsg)
	}

	encryptQueryParam := resp.Header.Get("x-encrypted-param")
	if encryptQueryParam == "" {
		return nil, fmt.Errorf("cdn upload succeeded but x-encrypted-param header missing")
	}

	return &UploadResult{
		Media: CDNMedia{
			EncryptQueryParam: encryptQueryParam,
			AESKey:            crypto.EncodeAESKeyBase64(aesKey),
			EncryptType:       1,
		},
		AESKey:            aesKey,
		EncryptedFileSize: len(ciphertext),
	}, nil
}

func cdnMediaMap(m *CDNMedia) map[string]interface{} {
	return map[string]interface{}{
		"encrypt_query_param": m.EncryptQueryParam,
		"aes_key":             m.AESKey,
		"encrypt_type":        m.EncryptType,
	}
}

func categorizeByExtension(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if imageExts[ext] {
		return "image"
	}
	if videoExts[ext] {
		return "video"
	}
	return "file"
}

func (b *Bot) sendText(ctx context.Context, userID, text, contextToken string) error {
	creds := b.getCreds()
	chunks := chunkText(text, 4000)
	for _, chunk := range chunks {
		msg := protocol.BuildTextMessage(userID, contextToken, chunk)
		if err := b.client.SendMessage(ctx, creds.BaseURL, creds.Token, msg); err != nil {
			return err
		}
	}
	return nil
}
