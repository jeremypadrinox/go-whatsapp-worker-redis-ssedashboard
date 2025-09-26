package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"gowhatsapp-worker/internal/models"
	"github.com/sirupsen/logrus"
	"net/textproto"
)

type Client struct {
	baseURL    string
	authHeader string
	httpClient *http.Client
	logger     *logrus.Logger
}

func NewClient(baseURL, authHeader string, logger *logrus.Logger) *Client {
	return &Client{
		baseURL:    baseURL,
		authHeader: authHeader,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (c *Client) SendMessage(ctx context.Context, target, message string) (*models.WhatsAppResponse, error) {
	if target == "" {
		return nil, fmt.Errorf("target phone (JID) is empty")
	}
	if !strings.Contains(target, "@") {
		target = target + "@s.whatsapp.net"
	}

	payload := models.SendMessageRequest{
		Phone:   target,
		Message: message,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/send/message", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	c.logger.WithFields(logrus.Fields{
		"endpoint": "/send/message",
		"status":   resp.StatusCode,
	}).Info("WhatsApp API response")

	var whatsappResp models.WhatsAppResponse
	if err := json.Unmarshal(body, &whatsappResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		whatsappResp.Success = false
		if whatsappResp.Message != "" {
			whatsappResp.Error = whatsappResp.Message
		} else if whatsappResp.Error == "" {
			whatsappResp.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return &whatsappResp, nil
	}

	if whatsappResp.Code == "SUCCESS" || whatsappResp.Results != nil {
		whatsappResp.Success = true
	} else {
		if whatsappResp.Code != "" && whatsappResp.Code != "SUCCESS" {
			whatsappResp.Success = false
			if whatsappResp.Error == "" {
				whatsappResp.Error = fmt.Sprintf("API returned error code: %s", whatsappResp.Code)
			}
		} else {
			whatsappResp.Success = true
		}
	}

	return &whatsappResp, nil
}

func (c *Client) SendImage(ctx context.Context, target, caption, imagePath string, viewOnce, compress bool, imageURL string) (*models.WhatsAppResponse, error) {
	if !strings.HasSuffix(target, "@s.whatsapp.net") {
		target = target + "@s.whatsapp.net"
	}

	if imageURL != "" && imagePath == "" {
		payload := map[string]interface{}{
			"phone":     target,
			"caption":   caption,
			"image_url": imageURL,
			"view_once": viewOnce,
			"compress":  compress,
		}
		jsonData, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/send/image", bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", c.authHeader)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		c.logger.WithFields(logrus.Fields{
			"endpoint": "/send/image",
			"status":   resp.StatusCode,
			"mode":     "json:image_url",
		}).Info("WhatsApp API response")
		var whatsappResp models.WhatsAppResponse
		if err := json.Unmarshal(body, &whatsappResp); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			whatsappResp.Success = false
			if whatsappResp.Message != "" {
				whatsappResp.Error = whatsappResp.Message
			} else if whatsappResp.Error == "" {
				whatsappResp.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
			}
			return &whatsappResp, nil
		}
		if whatsappResp.Code == "SUCCESS" || whatsappResp.Results != nil {
			whatsappResp.Success = true
		} else if whatsappResp.Code != "" && whatsappResp.Code != "SUCCESS" {
			whatsappResp.Success = false
			if whatsappResp.Error == "" {
				whatsappResp.Error = fmt.Sprintf("API returned error code: %s", whatsappResp.Code)
			}
		} else {
			whatsappResp.Success = true
		}
		return &whatsappResp, nil
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("phone", target); err != nil {
		return nil, fmt.Errorf("failed to write phone field: %w", err)
	}

	if caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return nil, fmt.Errorf("failed to write caption field: %w", err)
		}
	}

	if err := writer.WriteField("view_once", fmt.Sprintf("%t", viewOnce)); err != nil {
		return nil, fmt.Errorf("failed to write view_once field: %w", err)
	}

	if err := writer.WriteField("compress", fmt.Sprintf("%t", compress)); err != nil {
		return nil, fmt.Errorf("failed to write compress field: %w", err)
	}

	if imagePath != "" {
		file, err := os.Open(imagePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open image file: %w", err)
		}
		defer file.Close()

		fileInfo, err := file.Stat()
		if err != nil {
			return nil, fmt.Errorf("failed to get file info: %w", err)
		}

		var headerBuf [512]byte
		n, _ := io.ReadFull(file, headerBuf[:])
		detected := http.DetectContentType(headerBuf[:n])
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("failed to rewind file: %w", err)
		}

		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", fmt.Sprintf("form-data; name=\"%s\"; filename=\"%s\"", "image", fileInfo.Name()))
		h.Set("Content-Type", detected)
		part, err := writer.CreatePart(h)
		if err != nil {
			return nil, fmt.Errorf("failed to create multipart part: %w", err)
		}

		if _, err := io.Copy(part, file); err != nil {
			return nil, fmt.Errorf("failed to copy file content: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/send/image", &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	c.logger.WithFields(logrus.Fields{
		"endpoint": "/send/image",
		"status":   resp.StatusCode,
		"mode":     "multipart:file",
	}).Info("WhatsApp API response")

	var whatsappResp models.WhatsAppResponse
	if err := json.Unmarshal(body, &whatsappResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		whatsappResp.Success = false
		if whatsappResp.Message != "" {
			whatsappResp.Error = whatsappResp.Message
		} else if whatsappResp.Error == "" {
			whatsappResp.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return &whatsappResp, nil
	}

	if whatsappResp.Code == "SUCCESS" || whatsappResp.Results != nil {
		whatsappResp.Success = true
	} else {
		if whatsappResp.Code != "" && whatsappResp.Code != "SUCCESS" {
			whatsappResp.Success = false
			if whatsappResp.Error == "" {
				whatsappResp.Error = fmt.Sprintf("API returned error code: %s", whatsappResp.Code)
			}
		} else {
			whatsappResp.Success = true
		}
	}

	return &whatsappResp, nil
}

func (c *Client) SendFile(ctx context.Context, target, caption, filePath string) (*models.WhatsAppResponse, error) {
	if !strings.HasSuffix(target, "@s.whatsapp.net") {
		target = target + "@s.whatsapp.net"
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("phone", target); err != nil {
		return nil, fmt.Errorf("failed to write phone field: %w", err)
	}

	if caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return nil, fmt.Errorf("failed to write caption field: %w", err)
		}
	}

	if filePath != "" {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %w", err)
		}
		defer file.Close()

		fileInfo, err := file.Stat()
		if err != nil {
			return nil, fmt.Errorf("failed to get file info: %w", err)
		}

		part, err := writer.CreateFormFile("file", fileInfo.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to create form file: %w", err)
		}

		if _, err := io.Copy(part, file); err != nil {
			return nil, fmt.Errorf("failed to copy file content: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/send/file", &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	c.logger.WithFields(logrus.Fields{
		"endpoint": "/send/file",
		"status":   resp.StatusCode,
	}).Info("WhatsApp API response")

	var whatsappResp models.WhatsAppResponse
	if err := json.Unmarshal(body, &whatsappResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		whatsappResp.Success = false
		if whatsappResp.Message != "" {
			whatsappResp.Error = whatsappResp.Message
		} else if whatsappResp.Error == "" {
			whatsappResp.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return &whatsappResp, nil
	}

	if whatsappResp.Code == "SUCCESS" || whatsappResp.Results != nil {
		whatsappResp.Success = true
	} else {
		if whatsappResp.Code != "" && whatsappResp.Code != "SUCCESS" {
			whatsappResp.Success = false
			if whatsappResp.Error == "" {
				whatsappResp.Error = fmt.Sprintf("API returned error code: %s", whatsappResp.Code)
			}
		} else {
			whatsappResp.Success = true
		}
	}

	return &whatsappResp, nil
}

func (c *Client) SendTypingPresence(ctx context.Context, target string, isTyping bool) error {
	if !strings.HasSuffix(target, "@s.whatsapp.net") {
		target = target + "@s.whatsapp.net"
	}

	action := "stop"
	if isTyping {
		action = "start"
	}

	payload := models.TypingPresenceRequest{
		Phone:  target,
		Action: action,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal typing request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/send/chat-presence", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create typing request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send typing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("typing request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *Client) SendMessageWithTyping(ctx context.Context, target, message string, typingDelay time.Duration) (*models.WhatsAppResponse, error) {
	if err := c.SendTypingPresence(ctx, target, true); err != nil {
		return nil, fmt.Errorf("failed to start typing: %w", err)
	}

	time.Sleep(typingDelay)

	resp, err := c.SendMessage(ctx, target, message)
	if err != nil {
		c.SendTypingPresence(ctx, target, false)
		return nil, fmt.Errorf("failed to send message: %w", err)
	}

	if err := c.SendTypingPresence(ctx, target, false); err != nil {
	}

	return resp, nil
}

func (c *Client) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send health check request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("health check failed with status %d: %s", resp.StatusCode, string(body))
}

func (c *Client) GetBaseURL() string {
	return c.baseURL
}

func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}
