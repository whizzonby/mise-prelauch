package appnotify

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	// ErrUnregistered: the app was uninstalled or the token expired. Remove it.
	ErrUnregistered = errors.New("push token unregistered")
	// ErrInvalidToken: the token is malformed. Remove it.
	ErrInvalidToken = errors.New("push token invalid")
)

type PushMessage struct {
	Token, Platform string
	Title, Body     string
	Data            map[string]string
	// CollapseKey makes a resent message replace, not repeat, on the device.
	CollapseKey string
}

// Pusher delivers one message to one device.
type Pusher interface {
	Send(ctx context.Context, m PushMessage) error
}

// FCM sends through Firebase Cloud Messaging HTTP v1 (Android directly, iOS
// through APNs via Firebase). Credentials are a Google service account,
// kept on the server only.
type FCM struct {
	ProjectID   string
	ClientEmail string
	PrivateKey  *rsa.PrivateKey
	// Endpoints; tests point them at a fake.
	TokenURL string
	SendURL  string
	Client   *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

type serviceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// LoadFCM reads a service account JSON file. An empty path returns nil, nil:
// push stays off.
func LoadFCM(path string) (*FCM, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read FCM service account: %w", err)
	}
	return ParseFCM(b)
}

func ParseFCM(b []byte) (*FCM, error) {
	var sa serviceAccount
	if err := json.Unmarshal(b, &sa); err != nil {
		return nil, fmt.Errorf("parse FCM service account: %w", err)
	}
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil || sa.ProjectID == "" || sa.ClientEmail == "" {
		return nil, errors.New("FCM service account is incomplete")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("FCM private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("FCM private key is not RSA")
	}
	tokenURL := sa.TokenURI
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}
	return &FCM{
		ProjectID: sa.ProjectID, ClientEmail: sa.ClientEmail, PrivateKey: rk, TokenURL: tokenURL,
		SendURL: "https://fcm.googleapis.com/v1/projects/" + sa.ProjectID + "/messages:send",
		Client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// accessToken exchanges a signed JWT for an OAuth access token, cached
// until a minute before it expires.
func (f *FCM) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Now().Before(f.expires.Add(-time.Minute)) {
		return f.token, nil
	}
	now := time.Now()
	header := b64([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iss": f.ClientEmail, "scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud": f.TokenURL, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	signing := header + "." + b64(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.PrivateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {signing + "." + b64(sig)}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, f.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := f.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("FCM token exchange: status %d", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&tok); err != nil || tok.AccessToken == "" {
		return "", errors.New("FCM token exchange: bad response")
	}
	f.token, f.expires = tok.AccessToken, now.Add(time.Duration(tok.ExpiresIn)*time.Second)
	return f.token, nil
}

func (f *FCM) Send(ctx context.Context, m PushMessage) error {
	tok, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	msg := map[string]any{
		"token":        m.Token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
		"data":         m.Data,
		"android": map[string]any{
			"collapse_key": m.CollapseKey,
			"notification": map[string]any{"icon": "ic_stat_mise", "color": "#1E3A2B", "visibility": "PRIVATE"},
		},
		"apns": map[string]any{
			"headers": map[string]string{"apns-collapse-id": m.CollapseKey},
			"payload": map[string]any{"aps": map[string]any{"sound": "default"}},
		},
	}
	body, _ := json.Marshal(map[string]any{"message": msg})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, f.SendURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	res, err := f.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		return nil
	}
	var e struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&e)
	code := e.Error.Status
	for _, d := range e.Error.Details {
		if d.ErrorCode != "" {
			code = d.ErrorCode
		}
	}
	switch {
	case res.StatusCode == http.StatusNotFound || code == "UNREGISTERED":
		return ErrUnregistered
	case res.StatusCode == http.StatusBadRequest && code == "INVALID_ARGUMENT":
		return ErrInvalidToken
	}
	return fmt.Errorf("FCM send: status %d %s", res.StatusCode, code)
}
