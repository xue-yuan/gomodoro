package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	oauthListenAddr    = "127.0.0.1:8080"
	spotifyRedirectURI = "http://127.0.0.1:8080/callback"
	spotifyTokenURL    = "https://accounts.spotify.com/api/token"
	spotifyScopes      = "user-modify-playback-state user-read-playback-state"
)

type SpotifyTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

func newOAuthState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func getSpotifyAuthURL(clientID, state string) string {
	q := url.Values{
		"client_id":     {clientID},
		"response_type": {"code"},
		"redirect_uri":  {spotifyRedirectURI},
		"scope":         {spotifyScopes},
		"state":         {state},
	}
	return "https://accounts.spotify.com/authorize?" + q.Encode()
}

type oauthCallbackResult struct {
	code string
	err  error
}

func writeCallbackPage(w http.ResponseWriter, status int, color, heading, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<html><body style="font-family: sans-serif; text-align: center; padding-top: 50px;">
	<h1 style="color: %s;">%s</h1>
	<p>%s</p>
</body></html>`, color, html.EscapeString(heading), html.EscapeString(body))
}

func newOAuthCallbackHandler(expectedState string, results chan<- oauthCallbackResult) http.Handler {
	deliver := func(res oauthCallbackResult) {
		select {
		case results <- res:
		default:
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(expectedState)) != 1 {
			writeCallbackPage(w, http.StatusBadRequest, "#EF4444", "Authentication Failed", "Invalid or missing state parameter.")
			return
		}

		if authErr := q.Get("error"); authErr != "" {
			deliver(oauthCallbackResult{err: fmt.Errorf("spotify authentication error: %s", authErr)})
			writeCallbackPage(w, http.StatusBadRequest, "#EF4444", "Authentication Failed", "Error: "+authErr)
			return
		}

		code := q.Get("code")
		if code == "" {
			deliver(oauthCallbackResult{err: fmt.Errorf("no authorization code returned in callback")})
			writeCallbackPage(w, http.StatusBadRequest, "#EF4444", "Authentication Failed", "No authorization code returned.")
			return
		}

		deliver(oauthCallbackResult{code: code})
		writeCallbackPage(w, http.StatusOK, "#1DB954", "Authentication Successful!", "You can close this window now and return to your terminal.")
	})
	return mux
}

func StartOAuthServer(ctx context.Context, expectedState string, onListening func()) (string, error) {
	listener, err := net.Listen("tcp", oauthListenAddr)
	if err != nil {
		return "", fmt.Errorf("failed to start local OAuth server on %s: %v", oauthListenAddr, err)
	}

	results := make(chan oauthCallbackResult, 1)
	serveErr := make(chan error, 1)
	server := &http.Server{
		Handler:           newOAuthCallbackHandler(expectedState, results),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			serveErr <- fmt.Errorf("local OAuth server stopped: %v", err)
		}
	}()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if onListening != nil {
		onListening()
	}

	select {
	case res := <-results:
		return res.code, res.err
	case err := <-serveErr:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func requestSpotifyToken(ctx context.Context, clientID, clientSecret string, form url.Values) (*SpotifyTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, spotifyTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authHeader := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
	req.Header.Set("Authorization", "Basic "+authHeader)

	resp, err := spotifyHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errData)
		return nil, fmt.Errorf("status %d, error %v", resp.StatusCode, errData)
	}

	var tokenResp SpotifyTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	return &tokenResp, nil
}

func ExchangeCodeForToken(ctx context.Context, clientID, clientSecret, code string) (*SpotifyTokenResponse, error) {
	resp, err := requestSpotifyToken(ctx, clientID, clientSecret, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {spotifyRedirectURI},
	})
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %v", err)
	}
	return resp, nil
}

func RefreshSpotifyToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*SpotifyTokenResponse, error) {
	resp, err := requestSpotifyToken(ctx, clientID, clientSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %v", err)
	}
	if resp.RefreshToken == "" {
		resp.RefreshToken = refreshToken
	}
	return resp, nil
}
