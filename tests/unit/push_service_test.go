package unit

import (
	"context"
	"testing"

	"mobilevc/internal/push"
)

func TestNoopService(t *testing.T) {
	s := &push.NoopService{}
	err := s.SendNotification(context.Background(), push.NotificationRequest{
		Token: "test-token", Platform: "ios", Title: "hi", Body: "hello",
	})
	if err != nil {
		t.Fatalf("NoopService should never fail: %v", err)
	}
}

func TestNewAPNsService_NoAuth(t *testing.T) {
	_, err := push.NewAPNsService(push.APNsConfig{
		Topic: "com.test.app",
	})
	if err == nil {
		t.Fatal("expected error without AuthKeyPath or CertificatePath")
	}
}

func TestNewAPNsService_InvalidKeyPath(t *testing.T) {
	_, err := push.NewAPNsService(push.APNsConfig{
		AuthKeyPath: "/nonexistent/key.p8",
		KeyID:       "ABC123",
		TeamID:      "TEAM456",
		Topic:       "com.test.app",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent key file")
	}
}

func TestMockAPNsService_SendNotification(t *testing.T) {
	s := push.NewMockAPNsService()
	ctx := context.Background()

	err := s.SendNotification(ctx, push.NotificationRequest{
		Token: "tok-1", Platform: "ios", Title: "Test", Body: "test body",
		Data: map[string]string{"sessionId": "s1"},
	})
	if err != nil {
		t.Fatalf("MockAPNsService: %v", err)
	}

	if len(s.SentNotifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(s.SentNotifications))
	}
	n := s.SentNotifications[0]
	if n.Token != "tok-1" {
		t.Errorf("Token: got %q", n.Token)
	}
	if n.Platform != "ios" {
		t.Errorf("Platform: got %q", n.Platform)
	}
	if n.Title != "Test" {
		t.Errorf("Title: got %q", n.Title)
	}
	if n.Body != "test body" {
		t.Errorf("Body: got %q", n.Body)
	}
	if n.Data["sessionId"] != "s1" {
		t.Errorf("Data: %v", n.Data)
	}
}

func TestMockAPNsService_Multiple(t *testing.T) {
	s := push.NewMockAPNsService()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		s.SendNotification(ctx, push.NotificationRequest{Token: "t", Platform: "ios"})
	}
	if len(s.SentNotifications) != 3 {
		t.Errorf("expected 3, got %d", len(s.SentNotifications))
	}
}
