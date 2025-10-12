package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"gowhatsapp-worker/internal/config"
	"gowhatsapp-worker/internal/database"
	"gowhatsapp-worker/internal/models"
	"gowhatsapp-worker/internal/redis"
	"gowhatsapp-worker/internal/services"
	"gowhatsapp-worker/internal/utils"
	"gowhatsapp-worker/internal/whatsapp"
	"gowhatsapp-worker/internal/worker"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Server represents the unified web server
type Server struct {
	config         *config.Config
	db             database.DatabaseInterface
	whatsappClient whatsapp.MessageSender
	processor      *worker.Processor
	messageService *services.MessageService
	redisClient    *redis.Client
	fileStorage    *utils.FileStorage
	logger         *logrus.Logger
	server         *http.Server
	templates      *template.Template
}

// NewServer creates a new unified server instance with the provided dependencies.
// It initializes the server struct but does not start the HTTP server.
func NewServer(
	cfg *config.Config,
	db database.DatabaseInterface,
	wa whatsapp.MessageSender,
	proc *worker.Processor,
	msgSvc *services.MessageService,
	redisClient *redis.Client,
	logger *logrus.Logger,
) *Server {
	fileStorage := utils.NewFileStorage("tmp")

	return &Server{
		config:         cfg,
		db:             db,
		whatsappClient: wa,
		processor:      proc,
		messageService: msgSvc,
		redisClient:    redisClient,
		fileStorage:    fileStorage,
		logger:         logger,
	}
}

// Start starts the unified web server in a goroutine and waits for context cancellation.
// It loads templates, sets up routes and real-time logging, then listens on the configured address.
// Returns an error if template loading fails; otherwise, it calls Stop() upon context cancellation.
func (s *Server) Start(ctx context.Context) error {
	if err := s.loadTemplates(); err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	mux := http.NewServeMux()
	s.setupRoutes(mux)

	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%s", s.config.APIHost, s.config.APIPort),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	s.logger.Infof("🌐 Server running on http://%s", s.server.Addr)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("Server error: %v", err)
		}
	}()

	<-ctx.Done()
	return s.Stop()
}

// Stop gracefully shuts down the HTTP server with a 10-second timeout.
// It returns nil if the server is already nil or shutdown succeeds; otherwise, returns the shutdown error.
func (s *Server) Stop() error {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
	}
	return nil
}

// setupRoutes configures all HTTP routes for the server, including authentication middleware.
// Protected routes (main interface, dashboard, API endpoints, static files) use the authMiddleware.
// Unprotected routes include only the health check.
func (s *Server) setupRoutes(mux *http.ServeMux) {
	authMiddleware := AuthMiddleware(
		s.config.EnableAuth,
		s.config.DashboardUsername,
		s.config.DashboardPassword,
		s.config.DashboardRealm,
	)

	// Health check endpoint (unprotected)
	mux.HandleFunc("/health", s.handleHealth)

	mux.HandleFunc("/", authMiddleware(s.handleMainInterface))
	mux.HandleFunc("/dashboard", authMiddleware(s.handleDashboard))

	mux.HandleFunc("/api/send/message", authMiddleware(s.handleAPISendMessage))
	mux.HandleFunc("/api/send/image", authMiddleware(s.handleAPISendImage))
	mux.HandleFunc("/api/send/file", authMiddleware(s.handleAPISendFile))

	mux.HandleFunc("/api/statistics/stream", authMiddleware(s.handleAPIStatisticsStream))
	mux.HandleFunc("/api/endpoints/stats", authMiddleware(s.handleEndpointStats))

	mux.HandleFunc("/docs", authMiddleware(s.handleDocumentation))
	mux.HandleFunc("/docs/openapi.yml", authMiddleware(s.handleOpenAPISpec))
	mux.HandleFunc("/docs/swagger", authMiddleware(s.handleSwagger))
}

// loadTemplates loads and parses the HTML dashboard template with custom functions for formatting.
// Custom functions include formatTime, formatDuration (Indonesian-friendly), div, and mul.
// Returns an error if template parsing fails.
func (s *Server) loadTemplates() error {
	tmpl := template.New("")

	tmpl.Funcs(template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format("2006-01-02 15:04:05")
		},
		"formatDuration": func(d time.Duration) string {
			d = d.Round(time.Second)
			hours := int(d.Hours())
			minutes := int(d.Minutes()) % 60
			seconds := int(d.Seconds()) % 60

			if hours > 0 {
				return fmt.Sprintf("%d hours %d minutes", hours, minutes)
			} else if minutes > 0 {
				return fmt.Sprintf("%d minutes %d seconds", minutes, seconds)
			} else {
				return fmt.Sprintf("%d seconds", seconds)
			}
		},
		"div": func(a, b int64) float64 {
			if b == 0 {
				return 0
			}
			return float64(a) / float64(b)
		},
		"mul": func(a, b float64) float64 {
			return a * b
		},
	})
	mainTemplate := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Go-WhatsApp Worker Dashboard</title>
    <meta http-equiv="Cache-Control" content="no-cache, no-store, must-revalidate">
    <meta http-equiv="Pragma" content="no-cache">
    <meta http-equiv="Expires" content="0">
    <style>
        * {
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            margin: 0;
            padding: 0;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            color: #333;
        }
        
        .container {
            max-width: 1400px;
            margin: 0 auto;
            padding: 20px;
        }
        
        .dashboard {
            background: rgba(255, 255, 255, 0.95);
            backdrop-filter: blur(10px);
            border-radius: 20px;
            box-shadow: 0 20px 40px rgba(0, 0, 0, 0.1);
            overflow: hidden;
        }
        
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 40px 30px;
            text-align: center;
            position: relative;
        }
        
        .header::before {
            content: '';
            position: absolute;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: url('data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><pattern id="grain" width="100" height="100" patternUnits="userSpaceOnUse"><circle cx="25" cy="25" r="1" fill="white" opacity="0.1"/><circle cx="75" cy="75" r="1" fill="white" opacity="0.1"/><circle cx="50" cy="10" r="0.5" fill="white" opacity="0.1"/></pattern></defs><rect width="100" height="100" fill="url(%23grain)"/></svg>');
        }
        
        .header h1 {
            margin: 0;
            font-size: 3em;
            font-weight: 700;
            position: relative;
            z-index: 1;
        }
        
        .header p {
            margin: 15px 0 0 0;
            opacity: 0.9;
            font-size: 1.2em;
            position: relative;
            z-index: 1;
        }
        
        .content {
            padding: 40px 30px;
        }
        
        .section-title {
            font-size: 1.5em;
            font-weight: 600;
            color: #333;
            margin: 0 0 25px 0;
            padding-bottom: 10px;
            border-bottom: 3px solid #667eea;
            display: flex;
            align-items: center;
        }
        
        .section-title::before {
            content: '';
            width: 4px;
            height: 24px;
            background: #667eea;
            margin-right: 12px;
            border-radius: 2px;
        }
        
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
            gap: 25px;
            margin-bottom: 40px;
        }
        
        .stat-card {
            background: linear-gradient(135deg, #ffffff 0%, #f8f9fa 100%);
            padding: 25px;
            border-radius: 16px;
            border: 1px solid #e9ecef;
            box-shadow: 0 4px 20px rgba(0, 0, 0, 0.08);
            transition: all 0.3s ease;
            position: relative;
            overflow: hidden;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            text-align: center;
        }
        
        .stat-card::before {
            content: '';
            position: absolute;
            top: 0;
            left: 0;
            right: 0;
            height: 4px;
            background: linear-gradient(90deg, #667eea, #764ba2);
        }
        
        .stat-card:hover {
            transform: translateY(-2px);
            box-shadow: 0 8px 30px rgba(0, 0, 0, 0.12);
        }
        
        .stat-card h3 {
            margin: 0 0 15px 0;
            color: #495057;
            font-size: 0.85em;
            text-transform: uppercase;
            letter-spacing: 1px;
            font-weight: 600;
        }
        
        .stat-card .value {
            font-size: 2.5em;
            font-weight: 700;
            color: #667eea;
            margin-bottom: 8px;
            line-height: 1;
        }
        
        .stat-card small {
            color: #6c757d;
            font-size: 0.9em;
            font-weight: 500;
        }
        
        .status-indicator {
            display: inline-block;
            width: 14px;
            height: 14px;
            border-radius: 50%;
            margin-right: 10px;
            box-shadow: 0 0 10px rgba(40, 167, 69, 0.3);
        }
        
        .status-online { 
            background: linear-gradient(135deg, #28a745, #20c997);
            animation: pulse 2s infinite;
        }
        
        .status-offline { 
            background: linear-gradient(135deg, #dc3545, #fd7e14);
        }
        
        @keyframes pulse {
            0% { box-shadow: 0 0 0 0 rgba(40, 167, 69, 0.4); }
            70% { box-shadow: 0 0 0 10px rgba(40, 167, 69, 0); }
            100% { box-shadow: 0 0 0 0 rgba(40, 167, 69, 0); }
        }
        
        .api-section {
            background: linear-gradient(135deg, #f8f9fa 0%, #e9ecef 100%);
            padding: 30px;
            border-radius: 16px;
            margin-top: 30px;
            border: 1px solid #dee2e6;
        }
        
        .api-section h3 {
            margin-top: 0;
            color: #333;
            font-size: 1.3em;
            font-weight: 600;
        }
        
        /* Message Playground Styles */
        .playground-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 25px;
            margin-bottom: 40px;
        }
        
        .playground-card {
            background: linear-gradient(135deg, #ffffff 0%, #f8f9fa 100%);
            padding: 30px;
            border-radius: 20px;
            border: 1px solid #e9ecef;
            box-shadow: 0 4px 20px rgba(0, 0, 0, 0.08);
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            position: relative;
            overflow: hidden;
            cursor: pointer;
            text-align: center;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            min-height: 200px;
        }
        
        .playground-card::before {
            content: '';
            position: absolute;
            top: 0;
            left: 0;
            right: 0;
            height: 4px;
            background: linear-gradient(90deg, #667eea, #764ba2);
        }
        
        .playground-card:hover {
            transform: translateY(-5px) scale(1.02);
            box-shadow: 0 15px 40px rgba(0, 0, 0, 0.15);
            border-color: #667eea;
        }
        
        .playground-icon {
            font-size: 3em;
            margin-bottom: 15px;
            display: block;
            filter: drop-shadow(0 2px 4px rgba(0, 0, 0, 0.1));
        }
        
        .playground-card h3 {
            margin: 0 0 10px 0;
            color: #333;
            font-size: 1.4em;
            font-weight: 700;
        }
        
        .playground-card p {
            margin: 0 0 20px 0;
            color: #6c757d;
            font-size: 0.95em;
            line-height: 1.5;
        }
        
        .playground-action {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 8px 16px;
            border-radius: 20px;
            font-size: 0.85em;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.5px;
            transition: all 0.3s ease;
        }
        
        .playground-card:hover .playground-action {
            background: linear-gradient(135deg, #764ba2 0%, #667eea 100%);
            transform: scale(1.05);
        }
        
        /* Message Modal Styles - Enhanced & Modern */
        .message-modal {
            position: fixed;
            top: 0;
            left: 0;
            width: 100vw;
            height: 100vh;
            background: rgba(0, 0, 0, 0.6);
            backdrop-filter: blur(8px);
            z-index: 10000;
            display: none;
            align-items: center;
            justify-content: center;
            padding: 20px;
            opacity: 0;
            transition: all 0.4s cubic-bezier(0.34, 1.56, 0.64, 1);
        }

        .message-modal.active {
            display: flex;
            opacity: 1;
        }

        .message-modal-content {
            background: linear-gradient(135deg, #ffffff 0%, #fefefe 100%);
            border-radius: 24px;
            box-shadow:
                0 20px 60px rgba(0, 0, 0, 0.15),
                0 8px 32px rgba(0, 0, 0, 0.1),
                inset 0 1px 0 rgba(255, 255, 255, 0.8);
            max-width: 650px;
            width: 100%;
            max-height: 85vh;
            overflow: visible;
            position: relative;
            transform: scale(0.85) translateY(30px) rotate(-1deg);
            transition: all 0.5s cubic-bezier(0.34, 1.56, 0.64, 1);
            border: 1px solid rgba(255, 255, 255, 0.2);
            display: flex;
            flex-direction: column;
        }

        .message-modal.active .message-modal-content {
            transform: scale(1) translateY(0) rotate(0deg);
        }

        .message-modal-header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 28px 32px;
            border-radius: 24px 24px 0 0;
            display: flex;
            align-items: center;
            justify-content: space-between;
            position: relative;
            overflow: hidden;
        }

        .message-modal-header::before {
            content: '';
            position: absolute;
            top: -50%;
            left: -50%;
            width: 200%;
            height: 200%;
            background: radial-gradient(circle, rgba(255,255,255,0.1) 0%, transparent 70%);
            animation: headerGlow 8s ease-in-out infinite;
        }

        @keyframes headerGlow {
            0%, 100% { transform: scale(1); opacity: 0.3; }
            50% { transform: scale(1.2); opacity: 0.1; }
        }

        .message-modal-header h2 {
            margin: 0;
            font-size: 1.6em;
            font-weight: 700;
            display: flex;
            align-items: center;
            gap: 12px;
            position: relative;
            z-index: 1;
            text-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
        }

        .message-modal-close {
            background: rgba(255, 255, 255, 0.15);
            backdrop-filter: blur(10px);
            border: 1px solid rgba(255, 255, 255, 0.2);
            color: white;
            width: 40px;
            height: 40px;
            border-radius: 50%;
            cursor: pointer;
            font-size: 20px;
            font-weight: 300;
            display: flex;
            align-items: center;
            justify-content: center;
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            position: relative;
            z-index: 1;
            box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
        }

        .message-modal-close:hover {
            background: rgba(255, 255, 255, 0.25);
            transform: scale(1.05) rotate(90deg);
            box-shadow: 0 6px 20px rgba(0, 0, 0, 0.2);
        }

        .message-modal-body {
            padding: 32px;
            background: linear-gradient(180deg, #ffffff 0%, #fafbfc 100%);
            flex: 1;
            overflow-y: auto;
            overflow-x: hidden;
        }

        .form-group {
            margin-bottom: 28px;
            position: relative;
        }

        .form-group label {
            display: block;
            margin-bottom: 10px;
            font-weight: 600;
            color: #2d3748;
            font-size: 0.95em;
            letter-spacing: 0.025em;
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .form-group label::before {
            content: '';
            width: 4px;
            height: 16px;
            background: linear-gradient(135deg, #667eea, #764ba2);
            border-radius: 2px;
            flex-shrink: 0;
        }

        .form-group textarea,
        .form-group input[type="text"],
        .form-group input[type="number"] {
            width: 100%;
            padding: 16px 18px;
            border: 2px solid #e2e8f0;
            border-radius: 12px;
            font-size: 15px;
            font-family: inherit;
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            background: linear-gradient(135deg, #ffffff 0%, #f8fafc 100%);
            box-shadow: inset 0 1px 3px rgba(0, 0, 0, 0.1);
            position: relative;
            z-index: 1;
        }

        .form-group textarea:focus,
        .form-group input[type="text"]:focus,
        .form-group input[type="number"]:focus {
            outline: none;
            border-color: #667eea;
            background: #ffffff;
            box-shadow:
                0 0 0 3px rgba(102, 126, 234, 0.1),
                inset 0 1px 3px rgba(0, 0, 0, 0.05);
            transform: translateY(-1px);
        }

        .form-group textarea::placeholder,
        .form-group input::placeholder {
            color: #a0aec0;
            font-style: italic;
        }

        .form-group textarea {
            min-height: 120px;
            resize: vertical;
            line-height: 1.6;
        }

        .form-group input[type="file"] {
            padding: 12px 18px;
            background: linear-gradient(135deg, #ffffff 0%, #f8fafc 100%);
            cursor: pointer;
            border: 2px dashed #cbd5e0;
            border-radius: 12px;
            transition: all 0.3s ease;
        }

        .form-group input[type="file"]:hover {
            border-color: #667eea;
            background: rgba(102, 126, 234, 0.02);
        }

        .form-group small {
            display: block;
            margin-top: 8px;
            color: #718096;
            font-size: 0.85em;
            line-height: 1.4;
            display: flex;
            align-items: center;
            gap: 6px;
        }

        .form-group small::before {
            content: '💡';
            font-size: 12px;
        }

        .required-indicator {
            color: #e53e3e;
            font-weight: 700;
            font-size: 0.9em;
            margin-left: 4px;
        }

        .message-modal-footer {
            padding: 24px 32px;
            border-top: 1px solid #e2e8f0;
            background: linear-gradient(135deg, #f8fafc 0%, #f1f5f9 100%);
            display: flex;
            gap: 16px;
            justify-content: flex-end;
            border-radius: 0 0 24px 24px;
            flex-shrink: 0;
            margin-top: auto;
        }
        
        .btn {
            padding: 14px 28px;
            border: none;
            border-radius: 50px;
            font-size: 14px;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            text-transform: uppercase;
            letter-spacing: 0.5px;
            position: relative;
            overflow: hidden;
            box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
            display: inline-flex;
            align-items: center;
            justify-content: center;
            gap: 8px;
            min-height: 48px;
        }

        .btn span:first-child {
            font-size: 16px;
            line-height: 1;
            flex-shrink: 0;
        }

        .btn span:last-child {
            flex: 1;
            text-align: center;
        }

        .btn::before {
            content: '';
            position: absolute;
            top: 50%;
            left: 50%;
            width: 0;
            height: 0;
            background: rgba(255, 255, 255, 0.2);
            border-radius: 50%;
            transform: translate(-50%, -50%);
            transition: all 0.5s ease;
        }

        .btn:active::before {
            width: 300px;
            height: 300px;
        }

        .btn-primary {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            box-shadow:
                0 4px 15px rgba(102, 126, 234, 0.3),
                inset 0 1px 0 rgba(255, 255, 255, 0.1);
        }

        .btn-primary:hover {
            transform: translateY(-3px) scale(1.02);
            box-shadow:
                0 8px 25px rgba(102, 126, 234, 0.4),
                inset 0 1px 0 rgba(255, 255, 255, 0.2);
        }

        .btn-secondary {
            background: linear-gradient(135deg, #e2e8f0 0%, #cbd5e0 100%);
            color: #4a5568;
            border: 2px solid #e2e8f0;
        }

        .btn-secondary:hover {
            background: linear-gradient(135deg, #cbd5e0 0%, #a0aec0 100%);
            transform: translateY(-2px) scale(1.02);
            box-shadow: 0 6px 20px rgba(0, 0, 0, 0.15);
        }

        .btn:disabled {
            opacity: 0.5;
            cursor: not-allowed;
            transform: none !important;
            box-shadow: none !important;
        }

        .btn:disabled::before {
            display: none;
        }
        
        /* File upload area - Enhanced */
        .file-upload-area {
            border: 2px dashed #cbd5e0;
            border-radius: 16px;
            padding: 40px 30px;
            text-align: center;
            background: linear-gradient(135deg, #f8fafc 0%, #f1f5f9 100%);
            transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
            cursor: pointer;
            position: relative;
            overflow: hidden;
            box-shadow: inset 0 1px 3px rgba(0, 0, 0, 0.05);
        }

        .file-upload-area::before {
            content: '';
            position: absolute;
            top: -50%;
            left: -50%;
            width: 200%;
            height: 200%;
            background: radial-gradient(circle, rgba(102, 126, 234, 0.03) 0%, transparent 70%);
            opacity: 0;
            transition: opacity 0.4s ease;
        }

        .file-upload-area:hover {
            border-color: #667eea;
            background: linear-gradient(135deg, rgba(102, 126, 234, 0.02) 0%, rgba(118, 75, 162, 0.02) 100%);
            transform: translateY(-2px);
            box-shadow:
                0 8px 25px rgba(102, 126, 234, 0.1),
                inset 0 1px 3px rgba(0, 0, 0, 0.05);
        }

        .file-upload-area:hover::before {
            opacity: 1;
        }

        .file-upload-area.dragover {
            border-color: #667eea;
            background: linear-gradient(135deg, rgba(102, 126, 234, 0.08) 0%, rgba(118, 75, 162, 0.08) 100%);
            transform: translateY(-4px) scale(1.02);
            box-shadow:
                0 12px 35px rgba(102, 126, 234, 0.15),
                inset 0 1px 3px rgba(0, 0, 0, 0.05);
        }

        .file-upload-area.dragover::before {
            opacity: 1;
            background: radial-gradient(circle, rgba(102, 126, 234, 0.08) 0%, transparent 70%);
        }

        .file-upload-icon {
            font-size: 3em;
            margin-bottom: 15px;
            color: #a0aec0;
            filter: drop-shadow(0 2px 8px rgba(102, 126, 234, 0.1));
            transition: all 0.3s ease;
        }

        .file-upload-area:hover .file-upload-icon {
            color: #667eea;
            transform: scale(1.1);
        }

        .file-upload-text {
            color: #718096;
            font-size: 1em;
            font-weight: 500;
            margin: 0;
            line-height: 1.4;
        }

        .file-upload-area:hover .file-upload-text {
            color: #4a5568;
        }
        
        /* Loading state */
        .loading {
            position: relative;
            pointer-events: none;
        }
        
        .loading::after {
            content: '';
            position: absolute;
            top: 50%;
            left: 50%;
            width: 20px;
            height: 20px;
            margin: -10px 0 0 -10px;
            border: 2px solid #f3f3f3;
            border-top: 2px solid #667eea;
            border-radius: 50%;
            animation: spin 1s linear infinite;
        }
        
        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }
        
        /* Advanced Options Styles - Enhanced */
        .options-grid {
            display: grid;
            grid-template-columns: 1fr;
            gap: 24px;
            margin-top: 20px;
            padding: 24px;
            background: linear-gradient(135deg, #f8fafc 0%, #f1f5f9 100%);
            border-radius: 16px;
            border: 1px solid #e2e8f0;
        }

        .option-item {
            display: flex;
            flex-direction: column;
            gap: 10px;
            padding: 16px;
            background: rgba(255, 255, 255, 0.6);
            border-radius: 12px;
            border: 1px solid rgba(255, 255, 255, 0.8);
            backdrop-filter: blur(10px);
            transition: all 0.3s ease;
        }

        .option-item:hover {
            background: rgba(255, 255, 255, 0.8);
            transform: translateY(-2px);
            box-shadow: 0 4px 15px rgba(0, 0, 0, 0.08);
        }

        .option-item label {
            font-weight: 600;
            color: #2d3748;
            font-size: 0.9em;
            letter-spacing: 0.025em;
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .option-item label::before {
            content: '';
            width: 4px;
            height: 16px;
            background: linear-gradient(135deg, #667eea, #764ba2);
            border-radius: 2px;
            flex-shrink: 0;
        }

        .option-item input[type="text"],
        .option-item input[type="number"] {
            width: 100%;
            padding: 12px 16px;
            border: 2px solid #e2e8f0;
            border-radius: 10px;
            font-size: 14px;
            font-family: inherit;
            transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            background: linear-gradient(135deg, #ffffff 0%, #f8fafc 100%);
            box-shadow: inset 0 1px 3px rgba(0, 0, 0, 0.1);
        }

        .option-item input[type="text"]:focus,
        .option-item input[type="number"]:focus {
            outline: none;
            border-color: #667eea;
            background: #ffffff;
            box-shadow:
                0 0 0 3px rgba(102, 126, 234, 0.1),
                inset 0 1px 3px rgba(0, 0, 0, 0.05);
            transform: translateY(-1px);
        }

        .option-item input[type="text"]::placeholder,
        .option-item input[type="number"]::placeholder {
            color: #a0aec0;
            font-style: italic;
        }

        .option-item small {
            color: #718096;
            font-size: 0.85em;
            line-height: 1.4;
            display: flex;
            align-items: center;
            gap: 6px;
        }

        .option-item small::before {
            content: '💡';
            font-size: 11px;
        }

        /* Toggle Switch Styles - Modern */
        .toggle-label {
            display: flex;
            align-items: center;
            justify-content: space-between;
            cursor: pointer;
            font-weight: 600;
            color: #2d3748;
            font-size: 0.9em;
            padding: 8px 12px;
            border-radius: 12px;
            transition: all 0.3s ease;
            background: rgba(255, 255, 255, 0.5);
            border: 1px solid rgba(255, 255, 255, 0.8);
        }

        .toggle-label:hover {
            background: rgba(102, 126, 234, 0.05);
            border-color: rgba(102, 126, 234, 0.2);
        }

        .toggle-switch {
            position: relative;
            width: 52px;
            height: 28px;
            background: linear-gradient(135deg, #e2e8f0 0%, #cbd5e0 100%);
            border-radius: 14px;
            transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
            flex-shrink: 0;
            border: 2px solid rgba(255, 255, 255, 0.5);
            box-shadow: inset 0 1px 3px rgba(0, 0, 0, 0.1);
        }

        .toggle-switch::before {
            content: '';
            position: absolute;
            top: 3px;
            left: 3px;
            width: 18px;
            height: 18px;
            background: linear-gradient(135deg, #ffffff 0%, #f8fafc 100%);
            border-radius: 50%;
            transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
            box-shadow:
                0 2px 6px rgba(0, 0, 0, 0.2),
                inset 0 1px 0 rgba(255, 255, 255, 0.8);
        }

        .toggle-switch::after {
            content: '';
            position: absolute;
            top: 50%;
            left: 50%;
            transform: translate(-50%, -50%);
            width: 0;
            height: 0;
            background: rgba(102, 126, 234, 0.2);
            border-radius: 50%;
            transition: all 0.4s ease;
        }

        .toggle-label input[type="checkbox"] {
            display: none;
        }

        .toggle-label input[type="checkbox"]:checked + .toggle-switch {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            box-shadow:
                0 0 20px rgba(102, 126, 234, 0.3),
                inset 0 1px 0 rgba(255, 255, 255, 0.1);
        }

        .toggle-label input[type="checkbox"]:checked + .toggle-switch::before {
            transform: translateX(24px);
            background: linear-gradient(135deg, #ffffff 0%, #fefefe 100%);
            box-shadow:
                0 2px 8px rgba(0, 0, 0, 0.15),
                inset 0 1px 0 rgba(255, 255, 255, 0.9);
        }

        .toggle-label input[type="checkbox"]:checked + .toggle-switch::after {
            width: 40px;
            height: 40px;
            background: rgba(102, 126, 234, 0.1);
        }
        
        /* Small button styles */
        .btn-sm {
            padding: 8px 16px;
            font-size: 12px;
            border-radius: 20px;
        }
        
        /* Mobile responsiveness */
        @media (max-width: 768px) {
            .playground-grid {
                grid-template-columns: 1fr;
                gap: 15px;
            }

            .playground-card {
                padding: 20px;
                min-height: 160px;
            }

            .playground-icon {
                font-size: 2.5em;
            }

            .message-modal {
                padding: 8px;
            }

            .message-modal-content {
                margin: 8px;
                max-width: calc(100vw - 16px);
                max-height: calc(100vh - 16px);
                border-radius: 20px;
            }

            .message-modal-header {
                padding: 20px 24px;
            }

            .message-modal-header h2 {
                font-size: 1.4em;
            }

            .message-modal-close {
                width: 36px;
                height: 36px;
                font-size: 18px;
            }

            .message-modal-body {
                padding: 24px;
            }

            .form-group {
                margin-bottom: 24px;
            }

            .form-group label {
                font-size: 0.9em;
            }

            .form-group textarea,
            .form-group input[type="text"],
            .form-group input[type="number"] {
                padding: 14px 16px;
                font-size: 16px; /* Prevent zoom on iOS */
            }

            .file-upload-area {
                padding: 30px 20px;
            }

            .file-upload-icon {
                font-size: 2.5em;
            }

            .options-grid {
                grid-template-columns: 1fr;
                gap: 16px;
                padding: 20px;
                margin-top: 16px;
            }

            .option-item {
                gap: 8px;
                padding: 14px;
            }

            .option-item label {
                font-size: 0.85em;
            }

            .option-item input[type="text"],
            .option-item input[type="number"] {
                padding: 10px 14px;
                font-size: 16px; /* Prevent zoom on iOS */
            }

            .toggle-label {
                font-size: 0.85em;
                padding: 6px 10px;
                gap: 12px;
            }

            .toggle-switch {
                width: 48px;
                height: 26px;
                flex-shrink: 0;
            }

            .toggle-switch::before {
                width: 16px;
                height: 16px;
                top: 3px;
                left: 3px;
            }

            .toggle-label input[type="checkbox"]:checked + .toggle-switch::before {
                transform: translateX(22px);
            }

            .message-modal-footer {
                padding: 20px 24px;
                flex-direction: column;
                gap: 12px;
                flex-shrink: 0;
                margin-top: auto;
            }

            .btn {
                width: 100%;
                padding: 16px 24px;
                font-size: 15px;
            }

            .btn-primary,
            .btn-secondary {
                min-height: 50px;
            }
        }

        /* Extra small screens */
        @media (max-width: 480px) {
            .message-modal-body {
                padding: 20px;
            }

            .message-modal-header {
                padding: 16px 20px;
            }

            .message-modal-header h2 {
                font-size: 1.3em;
                gap: 8px;
            }

            .form-group textarea {
                min-height: 100px;
            }

            .file-upload-area {
                padding: 24px 16px;
            }

            .options-grid {
                padding: 16px;
            }

            .message-modal-footer {
                padding: 16px 20px;
                flex-shrink: 0;
                margin-top: auto;
            }
        }
        
        .api-endpoints {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 15px;
            margin-top: 20px;
        }
        
        .api-endpoint {
            background: white;
            padding: 20px;
            border-radius: 12px;
            border-left: 4px solid #667eea;
            box-shadow: 0 2px 10px rgba(0, 0, 0, 0.05);
            transition: all 0.3s ease;
        }
        
        .api-endpoint:hover {
            transform: translateX(5px);
            box-shadow: 0 4px 20px rgba(0, 0, 0, 0.1);
        }
        
        .api-endpoint strong {
            color: #667eea;
            font-weight: 600;
        }
        
        .api-endpoint code {
            background: #f8f9fa;
            padding: 4px 8px;
            border-radius: 6px;
            font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
            font-size: 0.9em;
            color: #495057;
            border: 1px solid #dee2e6;
        }
        
        .controls {
            display: flex;
            justify-content: center;
            margin-top: 30px;
            gap: 15px;
        }
        
        .refresh-btn {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            border: none;
            padding: 12px 24px;
            border-radius: 25px;
            cursor: pointer;
            font-size: 14px;
            font-weight: 600;
            transition: all 0.3s ease;
            box-shadow: 0 4px 15px rgba(102, 126, 234, 0.3);
        }
        
        .refresh-btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 6px 20px rgba(102, 126, 234, 0.4);
        }
                 .refresh-btn:active {
             transform: translateY(0);
         }
         
         .success-rate {
             background: linear-gradient(135deg, #28a745 0%, #20c997 100%);
             color: white;
         }
         
         .success-rate .value {
             color: white;
         }
         
         .success-rate h3 {
             color: rgba(255, 255, 255, 0.9);
         }
                  
                  .success-rate small {
              color: rgba(255, 255, 255, 0.8);
          }
                                  
                                 /* Full-Screen Presentation Mode - Completely Refactored */
           .presentation-mode {
               position: fixed;
               top: 0;
               left: 0;
               width: 100vw;
               height: 100vh;
               background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
               z-index: 99999;
               display: none;
               margin: 0;
               padding: 0;
                overflow: hidden;
           }
           
           .presentation-mode.active {
               display: block;
           }
           
           /* When presentation mode is active, hide body scroll */
           body.presentation-active {
               overflow: hidden !important;
               margin: 0 !important;
               padding: 0 !important;
               background: linear-gradient(135deg, #667eea 0%, #764ba2 100%) !important;
           }
           
           /* Ensure no other elements show through */
           body.presentation-active * {
               background: transparent !important;
           }
                     
                     .presentation-content {
               width: 100%;
                height: 100vh;
                padding: 30px;
               background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
               position: relative;
               display: flex;
               flex-direction: column;
                justify-content: space-between;
               margin: 0;
               color: #333;
                overflow: hidden;
           }
                     
                     .presentation-header {
               text-align: center;
               color: white;
               margin-bottom: 40px;
               background: rgba(255, 255, 255, 0.1);
               padding: 30px;
                border-radius: 20px;
                backdrop-filter: blur(20px);
                border: 1px solid rgba(255, 255, 255, 0.2);
                position: relative;
                overflow: hidden;
                box-shadow: 0 15px 35px rgba(0, 0, 0, 0.1);
            }
            
            .presentation-header::before {
                content: '';
                position: absolute;
                top: 0;
                left: 0;
                right: 0;
                bottom: 0;
                background: linear-gradient(45deg, transparent 30%, rgba(255, 255, 255, 0.1) 50%, transparent 70%);
                animation: shimmer 3s infinite;
            }
            
            @keyframes shimmer {
                0% { transform: translateX(-100%); }
                100% { transform: translateX(100%); }
           }
           
           .presentation-header h1 {
                font-size: 3.5em;
               margin: 0;
                text-shadow: 3px 3px 6px rgba(0,0,0,0.3);
               color: white;
                position: relative;
                z-index: 1;
                font-weight: 800;
                letter-spacing: -1px;
           }
           
           .presentation-header p {
                font-size: 1.4em;
                margin: 15px 0 0 0;
                opacity: 0.95;
                color: rgba(255, 255, 255, 0.95);
                position: relative;
                z-index: 1;
                font-weight: 400;
                text-shadow: 1px 1px 2px rgba(0,0,0,0.2);
           }
                     
                     .presentation-stats {
               display: grid;
                grid-template-columns: repeat(4, 1fr);
               gap: 25px;
                flex: 1;
                align-content: center;
                width: 100%;
                max-width: 1400px;
                margin: 0 auto;
                padding: 0;
                height: calc(100vh - 200px);
           }
                     
                     .presentation-stat {
                background: rgba(255, 255, 255, 0.95);
                padding: 25px;
                border-radius: 20px;
                border: 1px solid rgba(255, 255, 255, 0.3);
                box-shadow: 0 10px 30px rgba(0, 0, 0, 0.15);
                transition: all 0.4s cubic-bezier(0.4, 0, 0.2, 1);
                position: relative;
                overflow: hidden;
                text-align: center;
                display: flex;
                flex-direction: column;
                align-items: center;
                justify-content: center;
            }
           
            .presentation-stat::before {
                content: '';
                position: absolute;
                top: 0;
                left: 0;
                right: 0;
                height: 5px;
                background: linear-gradient(90deg, #667eea, #764ba2, #667eea);
                background-size: 200% 100%;
                animation: gradientShift 3s ease-in-out infinite;
            }
            
            @keyframes gradientShift {
                0%, 100% { background-position: 0% 50%; }
                50% { background-position: 100% 50%; }
           }
           
           .presentation-stat:hover {
                transform: translateY(-5px) scale(1.02);
                box-shadow: 0 20px 40px rgba(0, 0, 0, 0.2);
                background: rgba(255, 255, 255, 1);
           }
                     
                     .presentation-stat h3 {
               margin: 0 0 15px 0;
                color: white;
               font-size: 0.85em;
               text-transform: uppercase;
               letter-spacing: 1px;
                font-weight: 700;
                position: relative;
                z-index: 1;
                text-shadow: 1px 1px 2px rgba(0,0,0,0.3);
           }
                     
                     .presentation-stat .value {
                font-size: 2.2em;
                font-weight: 800;
                color: white;
               margin-bottom: 8px;
               line-height: 1;
                position: relative;
                z-index: 1;
                text-shadow: 2px 2px 4px rgba(0,0,0,0.3);
           }
                     
                     .presentation-stat small {
                color: rgba(255, 255, 255, 0.9);
                font-size: 0.8em;
               font-weight: 500;
                position: relative;
                z-index: 1;
                text-shadow: 1px 1px 2px rgba(0,0,0,0.2);
            }
            
            /* Success rate card special styling */
            .presentation-stat.success-rate {
                background: linear-gradient(135deg, rgba(40, 167, 69, 0.95) 0%, rgba(32, 201, 151, 0.95) 100%);
                color: white;
                border: 1px solid rgba(255, 255, 255, 0.4);
            }
            
            .presentation-stat.success-rate h3 {
                color: rgba(255, 255, 255, 0.9);
            }
            
            .presentation-stat.success-rate .value {
                color: white;
                text-shadow: 1px 1px 2px rgba(0,0,0,0.2);
            }
            
            .presentation-stat.success-rate small {
                color: rgba(255, 255, 255, 0.8);
            }
                        
                        .presentation-stat.success-rate::before {
                background: linear-gradient(90deg, #28a745, #20c997, #28a745);
            }
            
            /* Dynamic font sizing for time values */
            .presentation-stat .value.time-long {
                font-size: 1.6em;
            }
            
            .presentation-stat .value.time-medium {
                font-size: 1.8em;
            }
            
            .presentation-stat .value.time-short {
                font-size: 2.2em;
           }
          
          .fullscreen-btn {
              background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
              color: white;
              border: none;
              padding: 12px 24px;
              border-radius: 25px;
              cursor: pointer;
              font-size: 14px;
              font-weight: 600;
              transition: all 0.3s ease;
              box-shadow: 0 4px 15px rgba(102, 126, 234, 0.3);
          }
          
          .fullscreen-btn:hover {
              transform: translateY(-2px);
              box-shadow: 0 6px 20px rgba(102, 126, 234, 0.4);
          }
          
          .fullscreen-btn.presentation {
              background: linear-gradient(135deg, #dc3545 0%, #fd7e14 100%);
          }
          
          .fullscreen-btn.presentation:hover {
              box-shadow: 0 6px 20px rgba(220, 53, 69, 0.4);
          }
                                    
                                /* Presentation mode close button */
          .presentation-mode .fullscreen-btn {
              position: fixed !important;
               bottom: 30px !important;
               right: 30px !important;
              z-index: 100001 !important;
               padding: 15px !important;
               border-radius: 50% !important;
               font-size: 20px !important;
               width: 60px !important;
               height: 60px !important;
               display: flex !important;
               align-items: center !important;
               justify-content: center !important;
               background: rgba(220, 53, 69, 0.95) !important;
               backdrop-filter: blur(20px) !important;
               border: 2px solid rgba(255, 255, 255, 0.3) !important;
               box-shadow: 0 10px 30px rgba(220, 53, 69, 0.4) !important;
               transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1) !important;
               color: white !important;
               font-weight: bold !important;
           }
           
           .presentation-mode .fullscreen-btn:hover {
               transform: scale(1.1) !important;
               background: rgba(220, 53, 69, 1) !important;
               box-shadow: 0 15px 40px rgba(220, 53, 69, 0.6) !important;
           }
         
         
             margin-top: 30px;
         }
         
         .logs-container {
             background: linear-gradient(135deg, #f8f9fa 0%, #e9ecef 100%);
             border-radius: 16px;
             border: 1px solid #dee2e6;
             overflow: hidden;
         }
         
         .logs-header {
             background: #343a40;
             color: white;
             padding: 15px 20px;
             display: flex;
             justify-content: space-between;
             align-items: center;
         }
         
         
         
         .log-btn {
             background: rgba(255, 255, 255, 0.1);
             color: white;
             border: 1px solid rgba(255, 255, 255, 0.2);
             padding: 6px 12px;
             border-radius: 20px;
             cursor: pointer;
             font-size: 12px;
             font-weight: 500;
             transition: all 0.3s ease;
         }
         
         .log-btn:hover {
             background: rgba(255, 255, 255, 0.2);
         }
         
         .log-btn.active {
             background: #667eea;
             border-color: #667eea;
         }
         
         .logs-status {
             display: flex;
             align-items: center;
             gap: 8px;
             font-size: 12px;
         }
         
         
         
         
                   
                   
         
                   
          
          @keyframes fadeIn {
              from { opacity: 0; transform: translateY(-5px); }
              to { opacity: 1; transform: translateY(0); }
          }
         
         
         
         
         
         
         
         
         
         
         
         
                                       
                                       @media (max-width: 1200px) and (min-width: 769px) {
              .presentation-mode .presentation-stats {
                  grid-template-columns: repeat(3, 1fr);
                  gap: 20px;
                  max-width: 1000px;
              }
              
              .presentation-mode .presentation-stat {
                  padding: 20px;
                  min-height: 140px;
              }
              
              .presentation-mode .presentation-header h1 {
                  font-size: 3em;
              }
              
              .presentation-mode .presentation-header p {
                  font-size: 1.2em;
              }
         }
         
         @media (max-width: 768px) {
            .container {
                padding: 10px;
            }
            
            .header h1 {
                font-size: 2em;
            }
            
            .stats-grid {
                grid-template-columns: 1fr;
                gap: 15px;
            }
            
            .api-endpoints {
                grid-template-columns: 1fr;
            }
                                                                 /* Presentation mode mobile optimizations */
              .presentation-mode .presentation-content {
                  padding: 20px;
              }
              
              .presentation-mode .presentation-header {
                  padding: 20px;
                  margin-bottom: 30px;
              }
              
              .presentation-mode .presentation-header h1 {
                  font-size: 2.5em;
              }
              
              .presentation-mode .presentation-header p {
                  font-size: 1.1em;
              }
              
              .presentation-mode .presentation-stats {
                  grid-template-columns: repeat(2, 1fr);
                  gap: 15px;
                  max-width: 100%;
                  height: calc(100vh - 180px);
              }
              
              .presentation-mode .presentation-stat {
                  padding: 15px;
                  min-height: 120px;
              }
                                 .presentation-mode .presentation-stat .value {
                   font-size: 1.8em;
                   color: white;
               }
               
               .presentation-mode .presentation-stat .value.time-long {
                   font-size: 1.4em;
               }
               
               .presentation-mode .presentation-stat .value.time-medium {
                   font-size: 1.6em;
               }
               
               .presentation-mode .presentation-stat .value.time-short {
                   font-size: 1.8em;
               }
              
                             .presentation-mode .presentation-stat h3 {
                   font-size: 0.75em;
                   color: white;
               }
              
                             .presentation-mode .presentation-stat small {
                   font-size: 0.7em;
                   color: rgba(255, 255, 255, 0.9);
               }
              
              .presentation-mode .fullscreen-btn {
                  width: 50px !important;
                  height: 50px !important;
                  font-size: 18px !important;
                  bottom: 20px !important;
                  right: 20px !important;
              }
        }

        
    </style>
</head>
<body>

                 
    <div id="presentationMode" class="presentation-mode" style="display: none;">
          <button class="fullscreen-btn presentation" onclick="togglePresentationMode()" style="position: fixed; bottom: 20px; right: 20px; z-index: 100001;">
              ✕
         </button>
         <div class="presentation-content">
             <div class="presentation-header">
                <h1>Go-WhatsApp Live Monitoring</h1>
             </div>
             
                         <div class="presentation-stats">
                <div class="presentation-stat">
                    <h3>Worker Status</h3>
                     <div class="value">
                         <span class="status-indicator status-online"></span>
                         Online
                     </div>
                    <small>System running normally</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Processed (Session)</h3>
                     <div class="value" data-stat="pres-messages-processed">{{.Stats.MessagesProcessed}}</div>
                    <small>During this session</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Sent (Session)</h3>
                     <div class="value" data-stat="pres-messages-sent">{{.Stats.MessagesSent}}</div>
                    <small>Succeeded this session</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Failed (Session)</h3>
                     <div class="value" data-stat="pres-messages-failed">{{.Stats.MessagesFailed}}</div>
                    <small>Failed in this session</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Retries (Session)</h3>
                     <div class="value" data-stat="pres-retry-count">{{.Stats.RetryCount}}</div>
                    <small>During this session</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Uptime</h3>
                     <div class="value" data-stat="pres-uptime">{{formatDuration .Stats.Uptime}}</div>
                    <small>Session duration</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Throughput</h3>
                     <div class="value" data-stat="pres-redis-throughput">0</div>
                    <small>Messages per second</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Sent (Total)</h3>
                     <div class="value" data-stat="pres-total-sent">{{.Stats.TotalSent}}</div>
                    <small>Since start</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Failed (Total)</h3>
                     <div class="value" data-stat="pres-total-failed">{{.Stats.TotalFailed}}</div>
                    <small>Since start</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Queue Health</h3>
                     <div class="value" data-stat="pres-queue-health">Healthy</div>
                    <small>Normal / Degraded</small>
                 </div>
                 
                 <div class="presentation-stat">
                    <h3>Pending (Queue)</h3>
                     <div class="value" data-stat="pres-pending-messages">{{.Stats.PendingMessages}}</div>
                    <small>Current</small>
                 </div>
                                   
                                   <div class="presentation-stat success-rate">
                     <h3>Success Rate</h3>
                      <div class="value" data-stat="pres-success-rate">
                          {{if gt .Stats.TotalMessages 0}}
                              {{printf "%.1f%%" (mul (div .Stats.TotalSent .Stats.TotalMessages) 100)}}
                          {{else}}
                              0%
                          {{end}}
                      </div>
                     <small>Delivery success rate</small>
                  </div>
             </div>
         </div>
     </div>
     
         <div id="messageModal" class="message-modal">
         <div class="message-modal-content">
             <div class="message-modal-header">
                 <h2 id="modalTitle">
                     <span id="modalIcon">💬</span>
                     <span id="modalType">Send Message</span>
                 </h2>
                 <button class="message-modal-close" id="modalCloseBtn">&times;</button>
             </div>
             <div class="message-modal-body">
                 <form id="messageForm">
                                         <div class="form-group">
                        <label for="messageContent">
                            <span>Message Content</span>
                            <span class="required-indicator">*</span>
                        </label>
                         <textarea id="messageContent" name="message" placeholder="Enter your message here..." required></textarea>
                         <small id="messageHelp">Type your message content above</small>
                     </div>

                                         <div class="form-group" id="fileUploadGroup" style="display: none;">
                        <label for="fileInput">
                            <span>File Upload</span>
                            <span class="required-indicator">*</span>
                        </label>
                         <div class="file-upload-area" onclick="document.getElementById('fileInput').click()">
                             <div class="file-upload-icon">📎</div>
                             <div class="file-upload-text">Click to select file or drag and drop</div>
                             <input type="file" id="fileInput" name="file" style="display: none;" accept="*/*">
                         </div>
                         <small id="fileHelp">Select a file to send</small>
                     </div>

                                         <div class="form-group">
                        <label for="targetNumbers">
                            <span>Target Numbers</span>
                            <span class="required-indicator">*</span>
                        </label>
                         <textarea id="targetNumbers" name="targets" placeholder="Enter phone numbers, one per line&#10;Example:&#10;6281234567890&#10;6289876543210" required></textarea>
                         <small>Enter phone numbers in international format without + (e.g., 6281234567890), one per line</small>
                     </div>
                     
                                         <div class="form-group" id="advancedOptions" style="display: none;">
                        <label>Advanced Options</label>
                         <div class="options-grid">
                                                         <div class="option-item">
                                <label for="replyMessageID">Reply Message ID</label>
                                 <input type="text" id="replyMessageID" name="reply_message_id" placeholder="Optional reply message ID">
                                 <small>ID of message to reply to</small>
                             </div>
                             
                                                         <div class="option-item" id="durationOption" style="display: none;">
                                <label for="duration">Duration (seconds)</label>
                                 <input type="number" id="duration" name="duration" placeholder="0" min="0" max="300">
                                 <small>Duration for voice messages (0-300 seconds)</small>
                             </div>
                             
                                                         <div class="option-item" id="viewOnceOption" style="display: none;">
                                <label class="toggle-label">
                                     <input type="checkbox" id="viewOnce" name="view_once">
                                     <span class="toggle-switch"></span>
                                     View Once
                                 </label>
                                 <small>Image will disappear after viewing</small>
                             </div>
                             
                                                         <div class="option-item" id="compressOption" style="display: none;">
                                <label class="toggle-label">
                                     <input type="checkbox" id="compress" name="compress">
                                     <span class="toggle-switch"></span>
                                     Compress Image
                                 </label>
                                 <small>Compress image before sending</small>
                             </div>
                             
                                                         <div class="option-item">
                                <label class="toggle-label">
                                     <input type="checkbox" id="isForwarded" name="is_forwarded">
                                     <span class="toggle-switch"></span>
                                     Is Forwarded
                                 </label>
                                 <small>Mark message as forwarded</small>
                             </div>
                         </div>
                     </div>
                     
                                         <div class="form-group">
                        <button type="button" class="btn btn-secondary btn-sm" id="toggleAdvancedBtn" onclick="toggleAdvancedOptions()">
                             <span id="advancedToggleText">Show Advanced Options</span>
                         </button>
                     </div>
                 </form>
             </div>
             <div class="message-modal-footer">
                 <button type="button" class="btn btn-secondary" id="modalCancelBtn">
                     <span>❌</span>
                     Cancel
                 </button>
                 <button type="button" class="btn btn-primary" id="sendButton">
                     <span id="sendIcon">📤</span>
                     <span id="sendText">Send Message</span>
                 </button>
             </div>
         </div>
     </div>
     
     <div class="container">
         <div class="dashboard">
             <div class="header">
                 <h1>📱 Go-WhatsApp Worker</h1>
                 <p>High-performance WhatsApp message processing system</p>
             </div>
             
            <div class="content">
                               <h2 class="section-title">🎮 Message Playground</h2>
                <div class="playground-grid">
                    <div class="playground-card" data-message-type="text">
                        <div class="playground-icon">💬</div>
                        <h3>Send Message</h3>
                        <p>Send text messages to multiple recipients</p>
                        <div class="playground-action">Click to compose</div>
                    </div>
                    <div class="playground-card" data-message-type="image">
                        <div class="playground-icon">🖼️</div>
                        <h3>Send Image</h3>
                        <p>Send images with captions to recipients</p>
                        <div class="playground-action">Click to upload</div>
                    </div>
                    <div class="playground-card" data-message-type="file">
                        <div class="playground-icon">📎</div>
                        <h3>Send File</h3>
                        <p>Send documents and files to recipients</p>
                        <div class="playground-action">Click to attach</div>
                    </div>
                </div>

                              <h2 class="section-title">📊 Current Session</h2>
                <div class="stats-grid">
                    <div class="stat-card">
                        <h3>Worker Status</h3>
                        <div class="value">
                            <span class="status-indicator status-online"></span>
                            Online
                        </div>
                        <small>System is running</small>
                    </div>
                                         <div class="stat-card">
                        <h3>Processed (Session)</h3>
                         <div class="value" data-stat="messages-processed">{{.Stats.MessagesProcessed}}</div>
                        <small>This session</small>
                     </div>
                     <div class="stat-card">
                        <h3>Sent (Session)</h3>
                         <div class="value" data-stat="messages-sent">{{.Stats.MessagesSent}}</div>
                        <small>Successfully sent</small>
                     </div>
                     <div class="stat-card">
                        <h3>Failed (Session)</h3>
                         <div class="value" data-stat="messages-failed">{{.Stats.MessagesFailed}}</div>
                        <small>Failed attempts</small>
                     </div>
                     <div class="stat-card">
                        <h3>Retries (Session)</h3>
                         <div class="value" data-stat="retry-count">{{.Stats.RetryCount}}</div>
                        <small>Retry attempts</small>
                     </div>
                     <div class="stat-card">
                        <h3>Uptime</h3>
                         <div class="value" data-stat="uptime">{{formatDuration .Stats.Uptime}}</div>
                        <small>Session duration</small>
                     </div>
                </div>

                <h2 class="section-title">🗄️ Database Statistics</h2>
                <div class="stats-grid">
                                         <div class="stat-card">
                        <h3>Total Messages</h3>
                         <div class="value" data-stat="total-messages">{{.Stats.TotalMessages}}</div>
                        <small>All time</small>
                     </div>
                     <div class="stat-card">
                        <h3>Sent (Total)</h3>
                         <div class="value" data-stat="total-sent">{{.Stats.TotalSent}}</div>
                        <small>Successfully sent</small>
                     </div>
                     <div class="stat-card">
                        <h3>Failed (Total)</h3>
                         <div class="value" data-stat="total-failed">{{.Stats.TotalFailed}}</div>
                        <small>Delivery failed</small>
                     </div>
                     <div class="stat-card">
                        <h3>Total Retries</h3>
                         <div class="value" data-stat="total-retries">{{.Stats.TotalRetries}}</div>
                        <small>Retry attempts</small>
                     </div>
                     <div class="stat-card">
                        <h3>Pending (Queue)</h3>
                         <div class="value" data-stat="pending-messages">{{.Stats.PendingMessages}}</div>
                        <small>In queue</small>
                     </div>
                     <div class="stat-card success-rate">
                        <h3>Success Rate</h3>
                         <div class="value" data-stat="success-rate">
                             {{if gt .Stats.TotalMessages 0}}
                                 {{printf "%.1f%%" (mul (div .Stats.TotalSent .Stats.TotalMessages) 100)}}
                             {{else}}
                                 0%
                             {{end}}
                         </div>
                        <small>Delivery success rate</small>
                     </div>
                     <div class="stat-card">
                        <h3>DB Write Latency</h3>
                         <div class="value" data-stat="db-write-latency">0ms</div>
                        <small>Database write time</small>
                     </div>
                     <div class="stat-card">
                        <h3>Queue Backlog</h3>
                         <div class="value" data-stat="queue-backlog">0</div>
                        <small>Pending messages</small>
                     </div>
                            <div class="stat-card">
                                <h3>Messages / Minute</h3>
                                 <div class="value" data-stat="messages-per-minute">{{printf "%.1f" .Stats.MessagesPerMinute}}</div>
                                <small>Average throughput</small>
                            </div>
                </div>

                <h2 class="section-title">🔴 Redis Queue Statistics</h2>
                <div class="stats-grid">
                    <div class="stat-card">
                        <h3>Pending Messages</h3>
                        <div class="value">
                            <span data-stat="redis-pending-count">{{.Stats.RedisPendingCount}}</span>
                            <span data-stat="redis-queue-length" style="display: none;">{{.Stats.RedisQueueLength}}</span>
                        </div>
                        <small>Waiting for processing</small>
                    </div>
                    <div class="stat-card">
                        <h3>Queue Name</h3>
                        <div class="value" data-stat="redis-queue-name">{{.Stats.RedisQueueName}}</div>
                        <small>Redis queue name</small>
                    </div>
                    <div class="stat-card">
                        <h3>Redis Status</h3>
                        <div class="value">
                            <span class="status-indicator status-online"></span>
                            <span data-stat="redis-status">Connected</span>
                        </div>
                        <small>Redis connection</small>
                    </div>
                    <div class="stat-card">
                        <h3>Last Update</h3>
                        <div class="value" data-stat="redis-last-updated">{{formatTime .Stats.RedisLastUpdated}}</div>
                        <small>Last update time</small>
                    </div>
                    <div class="stat-card">
                        <h3>Throughput</h3>
                        <div class="value" data-stat="redis-throughput">0</div>
                        <small>Messages/sec</small>
                    </div>
                    <div class="stat-card">
                        <h3>Queue Health</h3>
                        <div class="value">
                            <span class="status-indicator status-online"></span>
                            <span data-stat="redis-health">Healthy</span>
                        </div>
                        <small>Queue health status</small>
                    </div>
                </div>

                <h2 class="section-title">🌐 Endpoint Statistics</h2>
                <div class="stats-grid" id="endpointStats">
                    <div class="stat-card">
                        <h3>Loading...</h3>
                        <div class="value">Fetching endpoint data</div>
                        <small>Please wait</small>
                    </div>
                </div>

                 				<div class="controls">
					<button class="refresh-btn" onclick="startManualRefresh()">
						🔄 Reload Data
					</button>
					<button class="refresh-btn fullscreen-btn" onclick="togglePresentationMode()">
						 📺 Full View
					</button>
					<a href="/docs" class="refresh-btn" style="text-decoration: none; display: inline-block;">
						 📚 System Docs
					</a>
					<a href="/docs/swagger" class="refresh-btn" style="text-decoration: none; display: inline-block;">
						 🧭 Swagger
					</a>
				</div>
            </div>
        </div>
    </div>

         <script>
         class ToastManager {
             constructor(options = {}) {
                 this.container = null;
                 this.toasts = new Map();
                 this.defaultDuration = options.duration || 5000;
                 this.position = options.position || 'top-right';
                 this.maxToasts = options.maxToasts || 5;
                 this.animationDuration = options.animationDuration || 300;
                 
                 this.init();
             }
             
             init() {
                 this.createContainer();
                 this.addStyles();
             }
             
             createContainer() {
                 const existing = document.getElementById('toast-container');
                 if (existing) existing.remove();
                 
                 this.container = document.createElement('div');
                 this.container.id = 'toast-container';
                 this.container.className = 'toast-container toast-' + this.position;
                 document.body.appendChild(this.container);
             }
             
             addStyles() {
                 if (document.getElementById('toast-styles')) return;
                 
                 const style = document.createElement('style');
                 style.id = 'toast-styles';
                 style.textContent = 
                     '.toast-container { position: fixed; z-index: 10000; pointer-events: none; max-width: 400px; }' +
                     '.toast-container.toast-top-right { top: 20px; right: 20px; }' +
                     '.toast { background: rgba(255, 255, 255, 0.95); backdrop-filter: blur(10px); border-radius: 12px; ' +
                              'box-shadow: 0 8px 32px rgba(0, 0, 0, 0.12); padding: 16px 20px; margin-bottom: 12px; ' +
                              'min-width: 320px; max-width: 400px; border-left: 4px solid #667eea; pointer-events: auto; ' +
                              'transform: translateX(100%); opacity: 0; transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1); ' +
                              'position: relative; overflow: hidden; }' +
                     '.toast.show { transform: translateX(0); opacity: 1; }' +
                     '.toast.hide { transform: translateX(100%); opacity: 0; }' +
                     '.toast.success { border-left-color: #28a745; background: rgba(40, 167, 69, 0.05); }' +
                     '.toast.error { border-left-color: #dc3545; background: rgba(220, 53, 69, 0.05); }' +
                     '.toast.warning { border-left-color: #ffc107; background: rgba(255, 193, 7, 0.05); }' +
                     '.toast.info { border-left-color: #17a2b8; background: rgba(23, 162, 184, 0.05); }' +
                     '.toast-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }' +
                     '.toast-icon { width: 20px; height: 20px; margin-right: 12px; flex-shrink: 0; }' +
                     '.toast-title { font-weight: 600; font-size: 14px; color: #333; margin: 0; flex: 1; }' +
                     '.toast-close { background: none; border: none; font-size: 18px; color: #6c757d; cursor: pointer; ' +
                                   'padding: 0; width: 20px; height: 20px; display: flex; align-items: center; ' +
                                   'justify-content: center; border-radius: 50%; transition: all 0.2s ease; }' +
                     '.toast-close:hover { background: rgba(108, 117, 125, 0.1); color: #495057; }' +
                     '.toast-message { font-size: 13px; color: #6c757d; line-height: 1.4; margin: 0; }' +
                     '.toast-progress { position: absolute; bottom: 0; left: 0; height: 3px; ' +
                                      'background: linear-gradient(90deg, #667eea, #764ba2); ' +
                                      'border-radius: 0 0 12px 12px; transition: width linear; }' +
                     '.toast.success .toast-progress { background: linear-gradient(90deg, #28a745, #20c997); }' +
                     '.toast.error .toast-progress { background: linear-gradient(90deg, #dc3545, #fd7e14); }' +
                     '.toast.warning .toast-progress { background: linear-gradient(90deg, #ffc107, #fd7e14); }' +
                     '.toast.info .toast-progress { background: linear-gradient(90deg, #17a2b8, #6f42c1); }' +
                     '@media (max-width: 768px) { .toast-container { top: 10px; right: 10px; left: 10px; max-width: none; } ' +
                                                '.toast { min-width: auto; max-width: none; width: 100%; } }';
                 document.head.appendChild(style);
             }
             
             show(options) {
                 const { type = 'info', title = '', message = '', duration = this.defaultDuration, 
                        closable = true, id = null } = options;
                 
                 const toastId = id || 'toast_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
                 
                 if (this.toasts.has(toastId)) this.hide(toastId);
                 if (this.toasts.size >= this.maxToasts) {
                     const firstToast = this.toasts.keys().next().value;
                     this.hide(firstToast);
                 }
                 
                 const toast = this.createToast(toastId, type, title, message, closable, duration);
                 this.container.appendChild(toast);
                 this.toasts.set(toastId, toast);
                 
                 requestAnimationFrame(() => toast.classList.add('show'));
                 
                 if (duration > 0) {
                     setTimeout(() => this.hide(toastId), duration);
                 }
                 
                 return toastId;
             }
             
             createToast(id, type, title, message, closable, duration) {
                 const toast = document.createElement('div');
                 toast.className = 'toast ' + type;
                 toast.setAttribute('data-toast-id', id);
                 
                 const icons = { success: '✅', error: '❌', warning: '⚠️', info: 'ℹ️' };
                 const progressBar = duration > 0 ? '<div class="toast-progress" style="width: 100%;"></div>' : '';
                 
                 toast.innerHTML = 
                     '<div class="toast-header">' +
                         '<div style="display: flex; align-items: center;">' +
                             '<span class="toast-icon">' + (icons[type] || icons.info) + '</span>' +
                             '<h4 class="toast-title">' + title + '</h4>' +
                         '</div>' +
                         (closable ? '<button class="toast-close" onclick="toastManager.hide(\'' + id + '\')">&times;</button>' : '') +
                     '</div>' +
                     '<p class="toast-message">' + message + '</p>' +
                     progressBar;
                 
                 if (duration > 0) {
                     const progressBar = toast.querySelector('.toast-progress');
                     if (progressBar) {
                         setTimeout(() => {
                             progressBar.style.width = '0%';
                             progressBar.style.transition = 'width ' + duration + 'ms linear';
                         }, 100);
                     }
                 }
                 
                 return toast;
             }
             
             hide(toastId) {
                 const toast = this.toasts.get(toastId);
                 if (!toast) return;
                 
                 toast.classList.remove('show');
                 toast.classList.add('hide');
                 
                 setTimeout(() => {
                     if (toast.parentNode) toast.parentNode.removeChild(toast);
                     this.toasts.delete(toastId);
                 }, this.animationDuration);
             }
             
             hideAll() {
                 for (const [id] of this.toasts) this.hide(id);
             }
             
             success(title, message, duration = this.defaultDuration) {
                 return this.show({ type: 'success', title, message, duration });
             }
             
             error(title, message, duration = this.defaultDuration) {
                 return this.show({ type: 'error', title, message, duration });
             }
             
             warning(title, message, duration = this.defaultDuration) {
                 return this.show({ type: 'warning', title, message, duration });
             }
             
             info(title, message, duration = this.defaultDuration) {
                 return this.show({ type: 'info', title, message, duration });
             }
         }
         
         const toastManager = new ToastManager();


        let __manualRefresh = false;
        function startManualRefresh() {
            __manualRefresh = true;
            startStatsStream();
            loadEndpointStats();
        }

        let updateInterval;
        let __statsES = null;

                  document.addEventListener('DOMContentLoaded', function() {
             document.body.style.opacity = '0';
             document.body.style.transition = 'opacity 0.5s ease-in-out';
             setTimeout(() => {
                 document.body.style.opacity = '1';
             }, 100);

             __manualRefresh = false;
             startStatsStream();
             loadEndpointStats();
        });

          function formatDuration(durationStr) {

              const match = durationStr.match(/(\d+(?:\.\d+)?)([hms])/g);
              if (!match) return durationStr;              let totalSeconds = 0;
              match.forEach(part => {
                  const value = parseFloat(part.slice(0, -1));
                  const unit = part.slice(-1);
                  switch (unit) {
                      case 'h':
                          totalSeconds += value * 3600;
                          break;
                      case 'm':
                          totalSeconds += value * 60;
                          break;
                      case 's':
                          totalSeconds += value;
                          break;
                  }
              });
              
              totalSeconds = Math.round(totalSeconds);
              
              const hours = Math.floor(totalSeconds / 3600);
              const minutes = Math.floor((totalSeconds % 3600) / 60);
              const seconds = totalSeconds % 60;
              
              if (hours > 0) {
                  return hours + " hours " + minutes + " minutes";
              } else if (minutes > 0) {
                  return minutes + " minutes " + seconds + " seconds";
              } else {
                  return seconds + " seconds";
              }
          }
          
          function loadEndpointStats() {
              fetch('/api/endpoints/stats')
                  .then(response => response.json())
                  .then(data => {
                      const container = document.getElementById('endpointStats');
                      if (!container) return;
                      
                      if (data.endpoints && data.endpoints.length > 0) {
                          container.innerHTML = data.endpoints.map(endpoint => {
                              const url = endpoint.url || 'Unknown';
                              const sent = endpoint.messages_sent || 0;
                              const failed = endpoint.messages_failed || 0;
                              const successRate = endpoint.success_rate || 0;
                              const avgResponseTime = endpoint.avg_response_time || 0;
                              const lastUsed = endpoint.last_used ? new Date(endpoint.last_used).toLocaleString() : 'Never';
                              
                              return '<div class="stat-card">' +
                                  '<h3>' + url + '</h3>' +
                                  '<div class="value">' + sent + '</div>' +
                                  '<small>Messages sent</small>' +
                                  '</div>' +
                                  '<div class="stat-card">' +
                                  '<h3>Success Rate</h3>' +
                                  '<div class="value">' + successRate.toFixed(1) + '%</div>' +
                                  '<small>For ' + url + '</small>' +
                                  '</div>' +
                                  '<div class="stat-card">' +
                                  '<h3>Failed</h3>' +
                                  '<div class="value">' + failed + '</div>' +
                                  '<small>For ' + url + '</small>' +
                                  '</div>' +
                                  '<div class="stat-card">' +
                                  '<h3>Avg Response</h3>' +
                                  '<div class="value">' + avgResponseTime + 'ms</div>' +
                                  '<small>For ' + url + '</small>' +
                                  '</div>' +
                                  '<div class="stat-card">' +
                                  '<h3>Last Used</h3>' +
                                  '<div class="value" style="font-size: 0.8em;">' + lastUsed + '</div>' +
                                  '<small>For ' + url + '</small>' +
                                  '</div>';
                          }).join('');
                      } else {
                          container.innerHTML = '<div class="stat-card">' +
                              '<h3>Single Endpoint</h3>' +
                              '<div class="value">Active</div>' +
                              '<small>Using single WhatsApp endpoint</small>' +
                              '</div>';
                      }
                  })
                  .catch(error => {
                      console.error('Failed to load endpoint stats:', error);
                      const container = document.getElementById('endpointStats');
                      if (container) {
                          container.innerHTML = '<div class="stat-card">' +
                              '<h3>Error</h3>' +
                              '<div class="value">Failed to load</div>' +
                              '<small>Endpoint statistics unavailable</small>' +
                              '</div>';
                      }
                  });
          }

          function startStatsStream() {


              if (typeof toastManager === 'undefined') {
                  console.error('❌ Toast manager not initialized!');
                  alert('Toast manager not initialized');
                  return;
              }             if (__statsES) {
                 try { __statsES.close(); } catch (e) {}
                 __statsES = null;
             }

             let loadingToast = null;
            if (__manualRefresh) {
                loadingToast = toastManager.info('Loading Data', 'Connecting to server...', 0);
             }             const es = new EventSource('/api/statistics/stream' + (__manualRefresh ? '?manual=1' : ''));
             __statsES = es;
              es.onmessage = (evt) => {
             if (loadingToast) {
                 toastManager.hide(loadingToast);
                 loadingToast = null;
             }
            if (__manualRefresh) {
                toastManager.success('Data Loaded', 'Statistics updated successfully', 3000);
                 __manualRefresh = false;
             }
                  
                  const data = JSON.parse(evt.data);
                  window.__lastSSEStats = data;
                  document.querySelector('[data-stat="messages-processed"]').textContent = data.worker.messages_processed;
                  document.querySelector('[data-stat="messages-sent"]').textContent = data.worker.messages_sent;
                  document.querySelector('[data-stat="messages-failed"]').textContent = data.worker.messages_failed;
                  document.querySelector('[data-stat="retry-count"]').textContent = data.worker.retry_count;

                  document.querySelector('[data-stat="total-messages"]').textContent = data.database.total_messages;
                  document.querySelector('[data-stat="total-sent"]').textContent = data.database.sent_messages;
                  document.querySelector('[data-stat="total-failed"]').textContent = data.database.failed_messages;
                  document.querySelector('[data-stat="total-retries"]').textContent = data.database.retry_count;
                  document.querySelector('[data-stat="pending-messages"]').textContent = data.database.pending_messages;

                  if (data.database.write_latency !== undefined) {
                      document.querySelector('[data-stat="db-write-latency"]').textContent = Math.round(data.database.write_latency / 1000000) + 'ms';
                  }
                  if (data.database.queue_backlog !== undefined) {
                      document.querySelector('[data-stat="queue-backlog"]').textContent = data.database.queue_backlog;
                  }

                  if (data.database.messages_per_minute !== undefined) {
                      const mpmEl = document.querySelector('[data-stat="messages-per-minute"]');
                      if (mpmEl) {
                          const value = Number(data.database.messages_per_minute) || 0;
                          mpmEl.textContent = value.toFixed(1);
                      }
                  }

                  const totalMessages = Number(data.database.total_messages) || 0;
                  const sentMessages = Number(data.database.sent_messages) || 0;
                  const successRateEl = document.querySelector('[data-stat="success-rate"]');
                  if (successRateEl) {
                      let successRateText = '0%';
                      if (totalMessages > 0) {
                          const successRate = Math.max(0, Math.min(100, (sentMessages / totalMessages) * 100));
                          successRateText = successRate.toFixed(1) + '%';
                      }
                      successRateEl.style.transform = 'scale(1.05)';
                      successRateEl.style.transition = 'transform 0.2s ease';
                      successRateEl.textContent = successRateText;
                      setTimeout(() => {
                          successRateEl.style.transform = 'scale(1)';
                      }, 200);
                  }

                  if (data.redis_queue) {
                      const pendingCount = Number(data.redis_queue.pending_count ?? 0);
                      const pendingEl = document.querySelector('[data-stat="redis-pending-count"]');
                      if (pendingEl) pendingEl.textContent = pendingCount;

                      const qLen = Number(data.redis_queue.queue_length ?? 0);
                      const queueLenEl = document.querySelector('[data-stat="redis-queue-length"]');
                      if (queueLenEl) queueLenEl.textContent = qLen;

                      const ts = data.redis_queue.last_updated;
                      if (ts) {
                          document.querySelector('[data-stat="redis-last-updated"]').textContent = ts;
                      }
                      const nameEl = document.querySelector('[data-stat="redis-queue-name"]');
                      if (nameEl && data.redis_queue.queue_name) {
                          nameEl.textContent = data.redis_queue.queue_name;
                      }
                      const statusEl = document.querySelector('[data-stat="redis-status"]');
                      if (statusEl) {
                          const isOnline = typeof qLen === 'number';
                          statusEl.textContent = isOnline ? 'Connected' : 'Disconnected';
                      }

                      if (typeof window.__lastPendingCount === 'undefined') {
                          window.__lastPendingCount = pendingCount;
                      }
                      const throughput = Math.max(0, pendingCount - window.__lastPendingCount);
                      window.__lastPendingCount = pendingCount;

                      const thrEl = document.querySelector('[data-stat="redis-throughput"]');
                      if (thrEl) thrEl.textContent = throughput;
                      const healthEl = document.querySelector('[data-stat="redis-health"]');
                      if (healthEl) {
                          const healthy = pendingCount === 0;
                          healthEl.textContent = healthy ? 'Healthy' : 'Backlog';
                      }
                  }

                    const uptimeElement = document.querySelector('[data-stat="uptime"]');
                    const formattedUptime = formatDuration(data.worker.uptime);
                    uptimeElement.textContent = formattedUptime;

                    uptimeElement.classList.remove('time-short', 'time-medium', 'time-long');
                   if (formattedUptime.includes('hours') || formattedUptime.includes('days')) {
                        uptimeElement.classList.add('time-long');
                   } else if (formattedUptime.includes('minutes')) {
                        uptimeElement.classList.add('time-medium');
                    } else {
                        uptimeElement.classList.add('time-short');
                    }
              };
                           es.onerror = () => {
                 if (loadingToast) {
                     toastManager.hide(loadingToast);
                     loadingToast = null;
                 }
                if (__manualRefresh) {
                    toastManager.error('Connection Failed', 'Failed to connect to server. Retrying...', 5000);
                 } 
                 try { es.close(); } catch (e) {}
                 __statsES = null;
                 setTimeout(() => { __manualRefresh = false; startStatsStream(); }, 3000);
             };
          }

          let isPresentationMode = false;
                     
                     function togglePresentationMode() {
               const presentationMode = document.getElementById('presentationMode');
               const fullscreenBtn = document.querySelector('.fullscreen-btn');
               const body = document.body;
               
               if (!isPresentationMode) {
                   presentationMode.classList.add('active');
                    fullscreenBtn.textContent = '✕';
                   fullscreenBtn.classList.add('presentation');
                   isPresentationMode = true;

                   document.querySelector('.container').style.display = 'none';
                   body.classList.add('presentation-active');
                   body.style.overflow = 'hidden';
                   body.style.margin = '0';
                   body.style.padding = '0';
                   presentationMode.style.display = 'block';
                   presentationMode.style.position = 'fixed';
                   presentationMode.style.top = '0';
                   presentationMode.style.left = '0';
                   presentationMode.style.width = '100vw';
                   presentationMode.style.height = '100vh';
                   presentationMode.style.zIndex = '99999';

                   if (document.documentElement.requestFullscreen) {
                       document.documentElement.requestFullscreen().catch(err => {

                       });
                   }

                   startPresentationUpdates();
               } else {
                   presentationMode.classList.remove('active');
                   fullscreenBtn.textContent = '📺 Full View';
                   fullscreenBtn.classList.remove('presentation');
                   isPresentationMode = false;

                   document.querySelector('.container').style.display = 'block';
                   body.classList.remove('presentation-active');
                   body.style.overflow = '';
                   body.style.margin = '';
                   body.style.padding = '';

                   presentationMode.style.display = 'none';

                   if (document.exitFullscreen) {
                       document.exitFullscreen().catch(err => {

                       });
                   }

                   stopPresentationUpdates();
               }
           }
          
          let presentationUpdateInterval;
          
          function startPresentationUpdates() {
              presentationUpdateInterval = setInterval(() => {
                  updatePresentationStats();
              }, 3000);
          }
          
          function stopPresentationUpdates() {
              if (presentationUpdateInterval) {
                  clearInterval(presentationUpdateInterval);
              }
          }
                     
                     function updatePresentationStats() {
             const nf = new Intl.NumberFormat('en-US');
             const data = window.__lastSSEStats;
             if (!data) { return; }
                 {
                       const updates = [
                           { selector: '[data-stat="pres-messages-processed"]', value: data.worker.messages_processed },
                           { selector: '[data-stat="pres-messages-sent"]', value: data.worker.messages_sent },
                           { selector: '[data-stat="pres-messages-failed"]', value: data.worker.messages_failed },
                           { selector: '[data-stat="pres-retry-count"]', value: data.worker.retry_count },
                           { selector: '[data-stat="pres-uptime"]', value: formatDuration(data.worker.uptime) },
                           { selector: '[data-stat="pres-total-sent"]', value: data.database.sent_messages },
                           { selector: '[data-stat="pres-total-failed"]', value: data.database.failed_messages },
                           { selector: '[data-stat="pres-pending-messages"]', value: data.database.pending_messages },
                           { selector: '[data-stat="pres-redis-pending-count"]', value: data.redis_queue ? data.redis_queue.pending_count : 0 }
                       ];

                       updates.forEach(update => {
                           const element = document.querySelector(update.selector);
                           if (element) {
                               element.style.transform = 'scale(1.05)';
                               element.style.transition = 'transform 0.2s ease';
                               const v = update.value;
                               const isNumeric = typeof v === 'number' && Number.isFinite(v);
                               element.textContent = isNumeric ? nf.format(v) : v;

                               if (update.selector === '[data-stat="pres-uptime"]') {
                                   element.classList.remove('time-short', 'time-medium', 'time-long');
                                  if (update.value.includes('hours') || update.value.includes('days')) {
                                       element.classList.add('time-long');
                                   } else if (update.value.includes('minutes')) {
                                       element.classList.add('time-medium');
                                   } else {
                                       element.classList.add('time-short');
                                   }
                               }
                               
                               setTimeout(() => {
                                   element.style.transform = 'scale(1)';
                               }, 200);
                           }
                       });
                       
                       const pendingPresCount = data.redis_queue ? (data.redis_queue.pending_count || 0) : 0;
                       if (typeof window.__lastPresPendingCount === 'undefined') window.__lastPresPendingCount = pendingPresCount;
                       const presThroughput = Math.max(0, pendingPresCount - window.__lastPresPendingCount);
                       window.__lastPresPendingCount = pendingPresCount;
                       const thrPresEl = document.querySelector('[data-stat="pres-redis-throughput"]');
                       if (thrPresEl) {
                           thrPresEl.style.transform = 'scale(1.05)';
                           thrPresEl.style.transition = 'transform 0.2s ease';
                           thrPresEl.textContent = nf.format(presThroughput);
                           setTimeout(() => { thrPresEl.style.transform = 'scale(1)'; }, 200);
                       }
                       const healthPresEl = document.querySelector('[data-stat="pres-queue-health"]');
                       if (healthPresEl) {
                           const healthy = pendingPresCount === 0;
                           healthPresEl.textContent = healthy ? 'Normal' : 'Degradasi';
                       }

                       const totalMessages = data.database.total_messages;
                       const sentMessages = data.database.sent_messages;
                       const successRateElement = document.querySelector('[data-stat="pres-success-rate"]');
                       if (successRateElement && totalMessages > 0) {
                           const successRate = ((sentMessages / totalMessages) * 100).toFixed(1);
                           successRateElement.style.transform = 'scale(1.05)';
                           successRateElement.style.transition = 'transform 0.2s ease';
                           successRateElement.textContent = successRate + '%';

                           setTimeout(() => {
                               successRateElement.style.transform = 'scale(1)';
                           }, 200);
                       }
                   }
           }

          document.addEventListener('fullscreenchange', function() {
              if (!document.fullscreenElement && isPresentationMode) {
                  togglePresentationMode();
              }
          });

          document.addEventListener('keydown', function(event) {
              if (event.key === 'Escape' && isPresentationMode) {
                  togglePresentationMode();
              }
          });

          document.addEventListener('click', function(event) {
              if (isPresentationMode && !event.target.closest('.presentation-mode') && !event.target.closest('.fullscreen-btn')) {
                  togglePresentationMode();
              }
          });
          
          let currentMessageType = 'text';
          
          function openMessageModal(type) {
              currentMessageType = type;
              const modal = document.getElementById('messageModal');
              const modalIcon = document.getElementById('modalIcon');
              const modalType = document.getElementById('modalType');
              const messageContent = document.getElementById('messageContent');
              const messageHelp = document.getElementById('messageHelp');
              const fileUploadGroup = document.getElementById('fileUploadGroup');
              const fileHelp = document.getElementById('fileHelp');
              const sendButton = document.getElementById('sendButton');
              const advancedOptions = document.getElementById('advancedOptions');
              const durationOption = document.getElementById('durationOption');
              const viewOnceOption = document.getElementById('viewOnceOption');
              const compressOption = document.getElementById('compressOption');
              
              document.getElementById('messageForm').reset();
              fileUploadGroup.style.display = 'none';
              advancedOptions.style.display = 'none';
              durationOption.style.display = 'none';
              viewOnceOption.style.display = 'none';
              compressOption.style.display = 'none';
              document.getElementById('advancedToggleText').textContent = 'Show Advanced Options';
              
              switch(type) {
                  case 'text':
                      modalIcon.textContent = '💬';
                      modalType.textContent = 'Send Text Message';
                      messageContent.placeholder = 'Enter your message here...';
                      messageHelp.textContent = 'Type your message content above';
                      sendButton.textContent = 'Send Message';
                      messageContent.required = true;
                      break;
                      
                  case 'image':
                      modalIcon.textContent = '🖼️';
                      modalType.textContent = 'Send Image';
                      messageContent.placeholder = 'Enter image caption (optional)...';
                      messageHelp.textContent = 'Add a caption for your image (optional)';
                      fileUploadGroup.style.display = 'block';
                      fileHelp.textContent = 'Select an image file (JPG, PNG, GIF, etc.)';
                      sendButton.textContent = 'Send Image';
                      messageContent.required = false;
                      document.getElementById('fileInput').accept = 'image/*';
                      viewOnceOption.style.display = 'block';
                      compressOption.style.display = 'block';
                      break;
                      
                  case 'file':
                      modalIcon.textContent = '📎';
                      modalType.textContent = 'Send File';
                      messageContent.placeholder = 'Enter file description (optional)...';
                      messageHelp.textContent = 'Add a description for your file (optional)';
                      fileUploadGroup.style.display = 'block';
                      fileHelp.textContent = 'Select any file to send';
                      sendButton.textContent = 'Send File';
                      messageContent.required = false;
                      document.getElementById('fileInput').accept = '*/*';
                      durationOption.style.display = 'block';
                      break;
              }
              
              modal.classList.add('active');
              document.body.style.overflow = 'hidden';
              
              setTimeout(() => {
                  if (type === 'text') {
                      messageContent.focus();
                  } else {
                      document.getElementById('fileInput').focus();
                  }
              }, 300);
          }
          
          function closeMessageModal() {
              const modal = document.getElementById('messageModal');
              modal.classList.remove('active');
              document.body.style.overflow = '';
              
              setTimeout(() => {
                  document.getElementById('messageForm').reset();
                  document.getElementById('fileUploadGroup').style.display = 'none';
                  document.getElementById('advancedOptions').style.display = 'none';
                  document.getElementById('advancedToggleText').textContent = 'Show Advanced Options';
              }, 300);
          }
          
          function toggleAdvancedOptions() {
              const advancedOptions = document.getElementById('advancedOptions');
              const toggleText = document.getElementById('advancedToggleText');
              
              if (advancedOptions.style.display === 'none' || advancedOptions.style.display === '') {
                  advancedOptions.style.display = 'block';
                  toggleText.textContent = 'Hide Advanced Options';
              } else {
                  advancedOptions.style.display = 'none';
                  toggleText.textContent = 'Show Advanced Options';
              }
          }
          
          function sendMessage() {
              const messageContent = document.getElementById('messageContent').value.trim();
              const targetNumbers = document.getElementById('targetNumbers').value.trim();
              const fileInput = document.getElementById('fileInput');
              const sendButton = document.getElementById('sendButton');
              
              const replyMessageID = document.getElementById('replyMessageID').value.trim();
              const duration = document.getElementById('duration').value;
              const viewOnce = document.getElementById('viewOnce').checked;
              const compress = document.getElementById('compress').checked;
              const isForwarded = document.getElementById('isForwarded').checked;
              
              if (!targetNumbers) {
                  toastManager.error('Validation Error', 'Please enter at least one target number', 4000);
                  document.getElementById('targetNumbers').focus();
                  return;
              }
              
              if (currentMessageType === 'text' && !messageContent) {
                  toastManager.error('Validation Error', 'Please enter a message', 4000);
                  document.getElementById('messageContent').focus();
                  return;
              }
              
              if ((currentMessageType === 'image' || currentMessageType === 'file') && !fileInput.files[0]) {
                  toastManager.error('Validation Error', 'Please select a file to send', 4000);
                  return;
              }
              
              const targets = targetNumbers.split('\n')
                  .map(num => num.trim())
                  .filter(num => num.length > 0);
              
              if (targets.length === 0) {
                  toastManager.error('Validation Error', 'Please enter valid phone numbers', 4000);
                  document.getElementById('targetNumbers').focus();
                  return;
              }
              
              const phoneRegex = /^[1-9]\d{1,14}$/;
              const invalidNumbers = targets.filter(num => !phoneRegex.test(num.replace(/\s/g, '')));
              
              if (invalidNumbers.length > 0) {
                  toastManager.error('Validation Error', 
                      'Invalid phone numbers: ' + invalidNumbers.join(', ') + '. Please use international format without + (e.g., 6281234567890)', 
                      6000);
                  return;
              }
              
              const sendIcon = document.getElementById('sendIcon');
              const sendText = document.getElementById('sendText');

              sendButton.disabled = true;
              sendButton.classList.add('loading');
              if (sendIcon) {
                  sendIcon.textContent = '⏳';
              }
              if (sendText) {
                  sendText.textContent = 'Sending...';
              } else {
                  sendButton.textContent = 'Sending...';
              }
              
              const phoneNumbers = targets.join(',');
              
              let requestPromise;
              
              if (currentMessageType === 'text') {
                  const payload = {
                      phone: phoneNumbers,
                      message: messageContent,
                      is_forwarded: isForwarded
                  };
                  
                  if (replyMessageID) payload.reply_message_id = replyMessageID;
                  
                  requestPromise = fetch('/api/send/message', {
                      method: 'POST',
                      headers: {
                          'Content-Type': 'application/json',
                      },
                      body: JSON.stringify(payload)
                  });
              } else if (currentMessageType === 'image') {
                  const formData = new FormData();
                  formData.append('phone', phoneNumbers);
                  formData.append('caption', messageContent);
                  formData.append('image', fileInput.files[0]);
                  formData.append('view_once', viewOnce.toString());
                  formData.append('compress', compress.toString());
                  formData.append('is_forwarded', isForwarded.toString());
                  
                  if (replyMessageID) formData.append('reply_message_id', replyMessageID);
                  
                  requestPromise = fetch('/api/send/image', {
                      method: 'POST',
                      body: formData
                  });
              } else if (currentMessageType === 'file') {
                  const formData = new FormData();
                  formData.append('phone', phoneNumbers);
                  formData.append('caption', messageContent);
                  formData.append('file', fileInput.files[0]);
                  formData.append('is_forwarded', isForwarded.toString());
                  
                  if (replyMessageID) formData.append('reply_message_id', replyMessageID);
                  if (duration) formData.append('duration', duration);
                  
                  requestPromise = fetch('/api/send/file', {
                      method: 'POST',
                      body: formData
                  });
              }
              
              requestPromise
                  .then(response => response.json())
                  .then(data => {
                      if (data.success) {
                          toastManager.success('Messages Queued', 
                              'Successfully queued ' + currentMessageType + ' for ' + targets.length + ' recipient(s). Messages will be processed in the background.', 
                              5000);
                          closeMessageModal();
                      } else {
                          throw new Error(data.error || 'Failed to queue messages');
                      }
                  })
                  .catch(error => {
                      console.error('Send message error:', error);
                      toastManager.error('Send Failed', 
                          'Failed to queue messages: ' + error.message, 
                          6000);
                  })
                  .finally(() => {
                      const sendIcon = document.getElementById('sendIcon');
                      const sendText = document.getElementById('sendText');

                      sendButton.disabled = false;
                      sendButton.classList.remove('loading');

                      const setButtonCopy = (iconValue, textValue) => {
                          if (sendIcon) {
                              sendIcon.textContent = iconValue;
                          }
                          if (sendText) {
                              sendText.textContent = textValue;
                          } else {
                              sendButton.textContent = iconValue + ' ' + textValue;
                          }
                      };

                      if (currentMessageType === 'text') {
                          setButtonCopy('💬', 'Send Message');
                      } else if (currentMessageType === 'image') {
                          setButtonCopy('🖼️', 'Send Image');
                      } else {
                          setButtonCopy('📎', 'Send File');
                      }
                  });
          }
          
          document.addEventListener('DOMContentLoaded', function() {
              document.addEventListener('click', function(e) {
                  const playgroundCard = e.target.closest('.playground-card');
                  if (playgroundCard && playgroundCard.dataset.messageType) {
                      openMessageModal(playgroundCard.dataset.messageType);
                  }
              });
              
              const modalCloseBtn = document.getElementById('modalCloseBtn');
              const modalCancelBtn = document.getElementById('modalCancelBtn');
              const sendButton = document.getElementById('sendButton');
              
              if (modalCloseBtn) {
                  modalCloseBtn.addEventListener('click', closeMessageModal);
              }
              
              if (modalCancelBtn) {
                  modalCancelBtn.addEventListener('click', closeMessageModal);
              }
              
              if (sendButton) {
                  sendButton.addEventListener('click', sendMessage);
              }
              
              const fileUploadArea = document.querySelector('.file-upload-area');
              const fileInput = document.getElementById('fileInput');
              
              if (fileUploadArea && fileInput) {
                  fileUploadArea.addEventListener('dragover', function(e) {
                      e.preventDefault();
                      fileUploadArea.classList.add('dragover');
                  });
                  
                  fileUploadArea.addEventListener('dragleave', function(e) {
                      e.preventDefault();
                      fileUploadArea.classList.remove('dragover');
                  });
                  
                  fileUploadArea.addEventListener('drop', function(e) {
                      e.preventDefault();
                      fileUploadArea.classList.remove('dragover');
                      
                      const files = e.dataTransfer.files;
                      if (files.length > 0) {
                          fileInput.files = files;
                          updateFileDisplay();
                      }
                  });
                  
                  fileInput.addEventListener('change', function() {
                      updateFileDisplay();
                  });
              }
          });
          
          function updateFileDisplay() {
              const fileInput = document.getElementById('fileInput');
              const fileUploadText = document.querySelector('.file-upload-text');
              
              if (fileInput.files && fileInput.files[0]) {
                  const file = fileInput.files[0];
                  const fileSize = (file.size / 1024 / 1024).toFixed(2);
                  fileUploadText.textContent = 'Selected: ' + file.name + ' (' + fileSize + ' MB)';
                  fileUploadText.style.color = '#28a745';
              } else {
                  fileUploadText.textContent = 'Click to select file or drag and drop';
                  fileUploadText.style.color = '#6c757d';
              }
          }
          
          document.addEventListener('keydown', function(event) {
              if (event.key === 'Escape') {
                  const modal = document.getElementById('messageModal');
                  if (modal.classList.contains('active')) {
                      closeMessageModal();
                  }
              }
          });
          
          document.addEventListener('click', function(event) {
              const modal = document.getElementById('messageModal');
              if (event.target === modal) {
                  closeMessageModal();
              }
          });
     </script>
</body>
</html>`

	var err error
	s.templates, err = tmpl.Parse(mainTemplate)
	return err
}

// handleMainInterface serves the main dashboard HTML template at the root path "/".
// It fetches current worker, database, and Redis statistics, calculates uptime, and executes the template.
// If the path is not "/", returns 404. On template execution error, returns 500.
func (s *Server) handleMainInterface(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	stats := s.processor.GetStats()

	dbStats, err := s.processor.GetMessageStats(r.Context())
	if err != nil {
		s.logger.Warnf("Failed to get database stats: %v", err)
		dbStats = &models.MessageStats{
			TotalMessages:      0,
			PendingMessages:    0,
			ProcessingMessages: 0,
			SentMessages:       0,
			FailedMessages:     0,
			RetryCount:         0,
		}
	}

	dbMetrics, err := s.db.GetDatabaseMetrics(r.Context())
	if err != nil {
		s.logger.Warnf("Failed to get database metrics: %v", err)
		dbMetrics = &models.DatabaseMetrics{
			WriteLatency:      0,
			QueueBacklog:      0,
			ActiveConnections: 0,
		}
	}

	redisStats := &redis.QueueStats{
		QueueLength:  0,
		PendingCount: 0,
		QueueName:    "unknown",
		LastUpdated:  time.Now(),
	}
	if s.redisClient != nil {
		if rs, err := s.redisClient.GetQueueStats(r.Context()); err != nil {
			s.logger.Warnf("Failed to get Redis stats: %v", err)
		} else {
			redisStats = rs
		}
	}

	uptime := time.Since(stats.StartTime)
	if stats.StartTime.IsZero() {
		uptime = 0
	}

	messagesPerMinute := 0.0
	if uptime > 0 {
		minutes := uptime.Minutes()
		if minutes > 0 {
			messagesPerMinute = float64(dbStats.SentMessages) / minutes
		}
	}
	data := struct {
		Stats struct {
			MessagesProcessed int64
			MessagesSent      int64
			MessagesFailed    int64
			RetryCount        int64
			Uptime            time.Duration
			TotalMessages     int64
			TotalSent         int64
			TotalFailed       int64
			TotalRetries      int64
			PendingMessages   int64
			RedisQueueLength  int64
			RedisPendingCount int64
			RedisQueueName    string
			RedisLastUpdated  time.Time
			DBWriteLatency    time.Duration
			QueueBacklog      int64
			MessagesPerMinute float64
		}
	}{
		Stats: struct {
			MessagesProcessed int64
			MessagesSent      int64
			MessagesFailed    int64
			RetryCount        int64
			Uptime            time.Duration
			TotalMessages     int64
			TotalSent         int64
			TotalFailed       int64
			TotalRetries      int64
			PendingMessages   int64
			RedisQueueLength  int64
			RedisPendingCount int64
			RedisQueueName    string
			RedisLastUpdated  time.Time
			DBWriteLatency    time.Duration
			QueueBacklog      int64
			MessagesPerMinute float64
		}{
			MessagesProcessed: stats.MessagesProcessed,
			MessagesSent:      stats.MessagesSent,
			MessagesFailed:    stats.MessagesFailed,
			RetryCount:        stats.RetryCount,
			Uptime:            uptime,
			TotalMessages:     dbStats.TotalMessages,
			TotalSent:         dbStats.SentMessages,
			TotalFailed:       dbStats.FailedMessages,
			TotalRetries:      dbStats.RetryCount,
			PendingMessages:   dbStats.PendingMessages,
			RedisQueueLength:  redisStats.QueueLength,
			RedisPendingCount: redisStats.PendingCount,
			RedisQueueName:    redisStats.QueueName,
			RedisLastUpdated:  redisStats.LastUpdated,
			DBWriteLatency:    dbMetrics.WriteLatency,
			QueueBacklog:      dbMetrics.QueueBacklog,
			MessagesPerMinute: messagesPerMinute,
		},
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.Execute(w, data); err != nil {
		s.logger.Errorf("Failed to execute template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleDashboard redirects requests to "/dashboard" to the root path "/" with a 301 status.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusMovedPermanently)
}

// handleHealth serves a simple "OK" response for health checks, unprotected by auth.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Simple health check - just return OK
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"gowhatsapp-worker"}`))
}

// handleAPISendMessage handles WhatsApp message sending requests (text messages)
func (s *Server) handleAPISendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req services.WhatsAppMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	if req.Phone == "" || req.Message == "" {
		http.Error(w, "Phone and message are required", http.StatusBadRequest)
		return
	}

	if strings.Contains(req.Phone, ",") {
		bulkReq := services.WhatsAppMessageBulkRequest{
			Phone:          req.Phone,
			Message:        req.Message,
			ReplyMessageID: req.ReplyMessageID,
			IsForwarded:    req.IsForwarded,
			Duration:       req.Duration,
		}

		response, err := s.messageService.ProcessWhatsAppMessageBulk(r.Context(), bulkReq)
		if err != nil {
			s.logger.WithError(err).Error("Failed to process WhatsApp bulk message request")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Internal server error",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	response, err := s.messageService.ProcessWhatsAppMessage(r.Context(), req)
	if err != nil {
		s.logger.WithError(err).Error("Failed to process WhatsApp message request")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Internal server error",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAPISendImage handles WhatsApp image sending requests
func (s *Server) handleAPISendImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
		return
	}

	phone := r.FormValue("phone")
	if phone == "" {
		http.Error(w, "Phone is required", http.StatusBadRequest)
		return
	}

	var filePath string
	if file, header, err := r.FormFile("image"); err == nil {
		defer file.Close()

		storedPath, err := s.fileStorage.StoreFile(header.Filename, file)
		if err != nil {
			s.logger.WithError(err).Error("Failed to store uploaded file")
			http.Error(w, "Failed to store uploaded file", http.StatusInternalServerError)
			return
		}
		filePath = storedPath
		s.logger.Infof("Stored image file: %s -> %s", header.Filename, filePath)
	}

	if strings.Contains(phone, ",") {
		bulkReq := services.WhatsAppMediaBulkRequest{
			Phone:       phone,
			Caption:     r.FormValue("caption"),
			ViewOnce:    r.FormValue("view_once") == "true",
			ImageURL:    r.FormValue("image_url"),
			Compress:    r.FormValue("compress") == "true",
			IsForwarded: r.FormValue("is_forwarded") == "true",
			FilePath:    filePath,
		}

		if durationStr := r.FormValue("duration"); durationStr != "" {
			if duration, err := strconv.Atoi(durationStr); err == nil {
				bulkReq.Duration = duration
			}
		}

		response, err := s.messageService.ProcessWhatsAppMediaBulk(r.Context(), bulkReq)
		if err != nil {
			s.logger.WithError(err).Error("Failed to process WhatsApp bulk media request")
			// Clean up stored file on error
			if filePath != "" {
				s.fileStorage.DeleteFile(filePath)
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Internal server error",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Handle as single request
	req := services.WhatsAppMediaRequest{
		Phone:       phone,
		Caption:     r.FormValue("caption"),
		ViewOnce:    r.FormValue("view_once") == "true",
		ImageURL:    r.FormValue("image_url"),
		Compress:    r.FormValue("compress") == "true",
		IsForwarded: r.FormValue("is_forwarded") == "true",
		FilePath:    filePath,
	}

	if durationStr := r.FormValue("duration"); durationStr != "" {
		if duration, err := strconv.Atoi(durationStr); err == nil {
			req.Duration = duration
		}
	}

	response, err := s.messageService.ProcessWhatsAppMedia(r.Context(), req)
	if err != nil {
		s.logger.WithError(err).Error("Failed to process WhatsApp media request")
		// Clean up stored file on error
		if filePath != "" {
			s.fileStorage.DeleteFile(filePath)
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Internal server error",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAPISendFile handles WhatsApp file sending requests
func (s *Server) handleAPISendFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Handle multipart form data for file uploads
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
		return
	}

	phone := r.FormValue("phone")
	if phone == "" {
		http.Error(w, "Phone is required", http.StatusBadRequest)
		return
	}

	// Handle file upload if present
	var filePath string
	if file, header, err := r.FormFile("file"); err == nil {
		defer file.Close()

		// Store the file in tmp directory
		storedPath, err := s.fileStorage.StoreFile(header.Filename, file)
		if err != nil {
			s.logger.WithError(err).Error("Failed to store uploaded file")
			http.Error(w, "Failed to store uploaded file", http.StatusInternalServerError)
			return
		}
		filePath = storedPath
		s.logger.Infof("Stored file: %s -> %s", header.Filename, filePath)
	}

	// Check if this is a bulk request (comma-separated phone numbers)
	if strings.Contains(phone, ",") {
		// Handle as bulk request
		bulkReq := services.WhatsAppFileBulkRequest{
			Phone:       phone,
			Caption:     r.FormValue("caption"),
			IsForwarded: r.FormValue("is_forwarded") == "true",
			FilePath:    filePath,
		}

		if durationStr := r.FormValue("duration"); durationStr != "" {
			if duration, err := strconv.Atoi(durationStr); err == nil {
				bulkReq.Duration = duration
			}
		}

		response, err := s.messageService.ProcessWhatsAppFileBulk(r.Context(), bulkReq)
		if err != nil {
			s.logger.WithError(err).Error("Failed to process WhatsApp bulk file request")
			// Clean up stored file on error
			if filePath != "" {
				s.fileStorage.DeleteFile(filePath)
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Internal server error",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Handle as single request
	req := services.WhatsAppFileRequest{
		Phone:       phone,
		Caption:     r.FormValue("caption"),
		IsForwarded: r.FormValue("is_forwarded") == "true",
		FilePath:    filePath,
	}

	if durationStr := r.FormValue("duration"); durationStr != "" {
		if duration, err := strconv.Atoi(durationStr); err == nil {
			req.Duration = duration
		}
	}

	response, err := s.messageService.ProcessWhatsAppFile(r.Context(), req)
	if err != nil {
		s.logger.WithError(err).Error("Failed to process WhatsApp file request")
		// Clean up stored file on error
		if filePath != "" {
			s.fileStorage.DeleteFile(filePath)
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Internal server error",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleAPIStatisticsStream(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("manual") == "1" {
		s.logger.Info("📊 Statistics stream requested")
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := s.processor.GetStats()
			dbStats, err := s.processor.GetMessageStats(r.Context())
			if err != nil {
				dbStats = &models.MessageStats{}
			}

			uptime := time.Since(stats.StartTime)
			if stats.StartTime.IsZero() {
				uptime = 0
			}

			messagesPerMinute := 0.0
			if uptime > 0 {
				minutes := uptime.Minutes()
				if minutes > 0 {
					messagesPerMinute = float64(dbStats.SentMessages) / minutes
				}
			}

			// Get database performance metrics
			dbMetrics, err := s.db.GetDatabaseMetrics(r.Context())
			if err != nil {
				dbMetrics = &models.DatabaseMetrics{
					WriteLatency:      0,
					QueueBacklog:      0,
					ActiveConnections: 0,
				}
			}

			redisStats := map[string]interface{}{"queue_length": 0, "pending_count": 0, "queue_name": "unknown", "last_updated": time.Now().Format("2006-01-02 15:04:05"), "redis_info": ""}
			if s.redisClient != nil {
				if rs, err := s.redisClient.GetQueueStats(r.Context()); err == nil {
					redisStats["queue_length"] = rs.QueueLength
					redisStats["pending_count"] = rs.PendingCount
					redisStats["queue_name"] = rs.QueueName
					redisStats["last_updated"] = rs.LastUpdated.Format("2006-01-02 15:04:05")
					redisStats["redis_info"] = rs.RedisInfo
				}
			}

			payload := map[string]interface{}{
				"worker": map[string]interface{}{
					"messages_processed": stats.MessagesProcessed,
					"messages_sent":      stats.MessagesSent,
					"messages_failed":    stats.MessagesFailed,
					"retry_count":        stats.RetryCount,
					"uptime":             uptime.String(),
					"last_processed_at":  stats.LastMessageTime,
				},
				"database": map[string]interface{}{
					"total_messages":      dbStats.TotalMessages,
					"pending_messages":    dbStats.PendingMessages,
					"processing_messages": dbStats.ProcessingMessages,
					"sent_messages":       dbStats.SentMessages,
					"failed_messages":     dbStats.FailedMessages,
					"retry_count":         dbStats.RetryCount,
					"write_latency":       dbMetrics.WriteLatency.Nanoseconds(),
					"queue_backlog":       dbMetrics.QueueBacklog,
					"active_connections":  dbMetrics.ActiveConnections,
					"messages_per_minute": messagesPerMinute,
				},
				"redis_queue": redisStats,
				"timestamp":   time.Now(),
			}

			b, _ := json.Marshal(payload)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			flusher.Flush()
		}
	}
}

// handleDocumentation serves the comprehensive system documentation HTML page
func (s *Server) handleDocumentation(w http.ResponseWriter, r *http.Request) {
	docPath := "docs/system-documentation.html"
	content, err := os.ReadFile(docPath)
	if err != nil {
		s.logger.Errorf("Failed to read documentation file: %v", err)
		http.Error(w, "Documentation not available", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	w.WriteHeader(http.StatusOK)
	w.Write(content)
}

// handleOpenAPISpec serves the OpenAPI YAML specification for the API
func (s *Server) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	specPath := "docs/openapi.yml"
	content, err := os.ReadFile(specPath)
	if err != nil {
		s.logger.Errorf("Failed to read OpenAPI spec file: %v", err)
		http.Error(w, "OpenAPI spec not available", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(http.StatusOK)
	w.Write(content)
}

// handleSwagger serves an embedded Swagger UI page pointing to /docs/openapi.yml
func (s *Server) handleSwagger(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>body { margin: 0; } #swagger-ui { height: 100vh; }</style>
  <meta http-equiv="Cache-Control" content="no-cache, no-store, must-revalidate">
  <meta http-equiv="Pragma" content="no-cache">
  <meta http-equiv="Expires" content="0">
  </head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.addEventListener('load', function() {
      window.ui = SwaggerUIBundle({
        url: '/docs/openapi.yml',
        dom_id: '#swagger-ui',
        presets: [SwaggerUIBundle.presets.apis],
        layout: 'BaseLayout'
      });
    });
  </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

// handleEndpointStats serves endpoint statistics for load balancer monitoring
func (s *Server) handleEndpointStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get stats from load balancer
	if lb, ok := s.whatsappClient.(*whatsapp.LoadBalancer); ok {
		stats := lb.GetAllStats()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"endpoints": stats,
		})
	} else {
		// Single endpoint
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"endpoints": []map[string]interface{}{{
				"url":             "single-endpoint",
				"messages_sent":   0,
				"messages_failed": 0,
				"success_rate":    100.0,
			}},
		})
	}
}
