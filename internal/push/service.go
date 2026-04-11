package push

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/certificate"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"
)

// NotificationRequest 推送请求
type NotificationRequest struct {
	Token    string
	Platform string
	Title    string
	Body     string
	Data     map[string]string
}

// Service 推送服务接口
type Service interface {
	SendNotification(ctx context.Context, req NotificationRequest) error
}

// NoopService 空实现（用于未配置推送时）
type NoopService struct{}

func (s *NoopService) SendNotification(ctx context.Context, req NotificationRequest) error {
	return nil
}

// APNsConfig APNs 配置
type APNsConfig struct {
	// 使用证书认证
	CertificatePath string
	CertificatePass string

	// 或使用 Token 认证（推荐）
	AuthKeyPath string
	KeyID       string
	TeamID      string

	// App Bundle ID
	Topic string

	// 是否使用生产环境
	Production bool
}

// APNsService iOS APNs 推送服务
type APNsService struct {
	client *apns2.Client
	topic  string
}

// NewAPNsService 创建 APNs 服务
func NewAPNsService(config APNsConfig) (*APNsService, error) {
	var client *apns2.Client

	// 优先使用 Token 认证（推荐方式）
	if config.AuthKeyPath != "" {
		authKey, err := token.AuthKeyFromFile(config.AuthKeyPath)
		if err != nil {
			return nil, fmt.Errorf("load auth key: %w", err)
		}

		tokenProvider := &token.Token{
			AuthKey: authKey,
			KeyID:   config.KeyID,
			TeamID:  config.TeamID,
		}

		if config.Production {
			client = apns2.NewTokenClient(tokenProvider).Production()
		} else {
			client = apns2.NewTokenClient(tokenProvider).Development()
		}
	} else if config.CertificatePath != "" {
		// 使用证书认证
		cert, err := certificate.FromP12File(config.CertificatePath, config.CertificatePass)
		if err != nil {
			return nil, fmt.Errorf("load certificate: %w", err)
		}

		if config.Production {
			client = apns2.NewClient(cert).Production()
		} else {
			client = apns2.NewClient(cert).Development()
		}
	} else {
		return nil, fmt.Errorf("either AuthKeyPath or CertificatePath must be provided")
	}

	return &APNsService{
		client: client,
		topic:  config.Topic,
	}, nil
}

func (s *APNsService) SendNotification(ctx context.Context, req NotificationRequest) error {
	if req.Platform != "ios" {
		return fmt.Errorf("APNsService only supports iOS platform, got: %s", req.Platform)
	}

	// 构造推送 payload
	p := payload.NewPayload().
		Alert(req.Title).
		AlertBody(req.Body).
		Sound("default").
		Badge(1)

	// 添加自定义数据
	for key, value := range req.Data {
		if key == "" || value == "" {
			continue
		}
		p.Custom(key, value)
	}
	if _, ok := req.Data["type"]; !ok {
		p.Custom("type", "action_needed")
	}

	notification := &apns2.Notification{
		DeviceToken: req.Token,
		Topic:       s.topic,
		Payload:     p,
		Priority:    apns2.PriorityHigh,
		Expiration:  time.Now().Add(24 * time.Hour),
	}

	// 发送推送
	res, err := s.client.PushWithContext(ctx, notification)
	if err != nil {
		return fmt.Errorf("push notification: %w", err)
	}

	// 检查响应
	if res.StatusCode != 200 {
		return fmt.Errorf("push failed: status=%d reason=%s", res.StatusCode, res.Reason)
	}

	return nil
}

// MockAPNsService 用于测试的 Mock 服务
type MockAPNsService struct {
	SentNotifications []MockNotification
}

type MockNotification struct {
	Token    string
	Platform string
	Title    string
	Body     string
	Data     map[string]string
	SentAt   time.Time
}

func (s *MockAPNsService) SendNotification(ctx context.Context, req NotificationRequest) error {
	s.SentNotifications = append(s.SentNotifications, MockNotification{
		Token:    req.Token,
		Platform: req.Platform,
		Title:    req.Title,
		Body:     req.Body,
		Data:     maps.Clone(req.Data),
		SentAt:   time.Now(),
	})
	return nil
}

// NewMockAPNsService 创建 Mock 服务（用于测试）
func NewMockAPNsService() *MockAPNsService {
	return &MockAPNsService{
		SentNotifications: make([]MockNotification, 0),
	}
}
