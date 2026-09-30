package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"onobill/internal/config"
	"onobill/internal/domain"
	"onobill/internal/repository"

	"github.com/golang-jwt/jwt/v5"
)

type Service struct {
	repo   *repository.Repository
	secret []byte
}

func NewService(repo *repository.Repository, secret string) *Service {
	return &Service{repo: repo, secret: []byte(secret)}
}

type Claims struct {
	UserID   uint   `json:"user_id"`
	TenantID uint   `json:"tenant_id"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("email already registered")
)

// Login verifies credentials and returns a JWT token
func (s *Service) Login(email, password string) (string, *domain.User, error) {
	user, err := s.repo.GetUserByEmail(strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return "", nil, ErrInvalidCredentials
	}
	if !config.CheckPassword(user.PasswordHash, password) {
		return "", nil, ErrInvalidCredentials
	}

	token, err := s.GenerateToken(user)
	if err != nil {
		return "", nil, err
	}

	s.repo.CreateAuditLog(&domain.AuditLog{
		TenantID:   user.TenantID,
		UserID:     &user.ID,
		Action:     "login",
		Resource:   "user",
		ResourceID: user.ID,
	})

	return token, user, nil
}

// Register creates a new user under an existing tenant
func (s *Service) Register(tenantID uint, email, password, name, role string) (*domain.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if _, err := s.repo.GetUserByEmail(email); err == nil {
		return nil, ErrEmailTaken
	}

	hash, err := config.HashPassword(password)
	if err != nil {
		return nil, err
	}

	if role == "" {
		role = "admin"
	}

	user := &domain.User{
		TenantID:     tenantID,
		Email:        email,
		PasswordHash: hash,
		Name:         name,
		Role:         role,
	}
	if err := s.repo.CreateUser(user); err != nil {
		return nil, err
	}
	return user, nil
}

// GenerateToken creates a JWT for the user (valid 24h)
func (s *Service) GenerateToken(user *domain.User) (string, error) {
	claims := Claims{
		UserID:   user.ID,
		TenantID: user.TenantID,
		Email:    user.Email,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "onobill",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// ParseToken validates a JWT and returns claims
func (s *Service) ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// Middleware: extract JWT from cookie or Authorization header
func (s *Service) GetClaims(r *http.Request) (*Claims, error) {
	// Try cookie first
	if c, err := r.Cookie("onobill_token"); err == nil && c.Value != "" {
		return s.ParseToken(c.Value)
	}
	// Try Authorization: Bearer xxx
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return s.ParseToken(strings.TrimPrefix(auth, "Bearer "))
	}
	return nil, errors.New("no token provided")
}

// SetAuthCookie writes JWT to httpOnly cookie
func (s *Service) SetAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "onobill_token",
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearAuthCookie removes the auth cookie
func (s *Service) ClearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "onobill_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}
