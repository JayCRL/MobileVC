package auth

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

type Claims struct {
	jwt.RegisteredClaims
	UserID   string `json:"uid"`
	Provider string `json:"prv"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
}

type JWTService struct {
	secret          []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL int // days
}

func NewJWTService(secret string, accessTokenTTLMin, refreshTokenTTLDays int) *JWTService {
	return &JWTService{
		secret:          []byte(secret),
		accessTokenTTL:  time.Duration(accessTokenTTLMin) * time.Minute,
		refreshTokenTTL: refreshTokenTTLDays,
	}
}

func (s *JWTService) GenerateAccessToken(userID, provider, name, email string) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.New().String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTokenTTL)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		},
		UserID:   userID,
		Provider: provider,
		Name:     name,
		Email:    email,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *JWTService) ParseAccessToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{},
		func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return s.secret, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

func (s *JWTService) AccessTokenTTLMinutes() int {
	return int(s.accessTokenTTL.Minutes())
}

func (s *JWTService) RefreshTokenTTLDays() int {
	return s.refreshTokenTTL
}

// DecodeTokenPayload extracts the payload without verification.
// Used by the Flutter client to read user info without verifying signature.
func DecodeTokenPayload(tokenString string) (map[string]interface{}, error) {
	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}
	result := make(map[string]interface{})
	for k, v := range claims {
		result[k] = v
	}
	// Add a JSON-friendly decoded representation
	payload := struct {
		UserID   string `json:"uid"`
		Provider string `json:"prv"`
		Name     string `json:"name"`
		Email    string `json:"email"`
	}{}
	if raw, err := json.Marshal(claims); err == nil {
		json.Unmarshal(raw, &payload)
	}
	result["user_id"] = payload.UserID
	result["provider"] = payload.Provider
	result["name"] = payload.Name
	result["email"] = payload.Email
	return result, nil
}
