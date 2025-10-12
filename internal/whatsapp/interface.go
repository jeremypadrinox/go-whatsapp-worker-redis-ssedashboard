package whatsapp

import (
	"context"
	"gowhatsapp-worker/internal/models"
)

type MessageSender interface {
	SendMessage(ctx context.Context, target, message string) (*models.WhatsAppResponse, error)
	SendImage(ctx context.Context, target, caption, imagePath string, viewOnce, compress bool, imageURL string) (*models.WhatsAppResponse, error)
	SendFile(ctx context.Context, target, caption, filePath string) (*models.WhatsAppResponse, error)
	SendTypingPresence(ctx context.Context, target string, isTyping bool) error
	HealthCheck(ctx context.Context) error
	Close() error
}
