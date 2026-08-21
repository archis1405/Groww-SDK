package groww

import (
	"net/http"
	"sync"
	"time"
)

const refreshSkew = 60 * time.Second

type StaticToken string

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
}

type RefreshingTokenSource struct {
	apiKey     string
	apiSecret  string
	baseURL    string
	httpClient *http.Client
	mu         sync.Mutex
	token      string
	expiry     time.Time
}
