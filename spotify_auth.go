package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type SpotifyTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

func StartOAuthServer(ctx context.Context) (string, error) {
	mux := http.NewServeMux()
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		stateErr := r.URL.Query().Get("error")

		if stateErr != "" {
			errChan <- fmt.Errorf("spotify authentication error: %s", stateErr)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<html><body style="font-family: sans-serif; text-align: center; padding-top: 50px;">
				<h1 style="color: #EF4444;">Authentication Failed</h1>
				<p>Error: ` + stateErr + `</p>
			</body></html>`))
			return
		}

		if code == "" {
			errChan <- fmt.Errorf("no authorization code returned in callback")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		codeChan <- code
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body style="font-family: sans-serif; text-align: center; padding-top: 50px;">
			<h1 style="color: #1DB954;">Authentication Successful!</h1>
			<p>You can close this window now and return to your terminal.</p>
		</body></html>`))
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- fmt.Errorf("failed to start local OAuth server: %v", err)
		}
	}()

	select {
	case code := <-codeChan:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return code, nil
	case err := <-errChan:
		_ = server.Shutdown(context.Background())
		return "", err
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return "", ctx.Err()
	}
}

func ExchangeCodeForToken(clientID, clientSecret, code string) (*SpotifyTokenResponse, error) {
	tokenURL := "https://accounts.spotify.com/api/token"
	redirectURI := "http://127.0.0.1:8080/callback"

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authHeader := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
	req.Header.Set("Authorization", "Basic "+authHeader)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errData)
		return nil, fmt.Errorf("token exchange failed: status %d, error %v", resp.StatusCode, errData)
	}

	var tokenResp SpotifyTokenResponse
	err = json.NewDecoder(resp.Body).Decode(&tokenResp)
	if err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

func RefreshSpotifyToken(clientID, clientSecret, refreshToken string) (*SpotifyTokenResponse, error) {
	tokenURL := "https://accounts.spotify.com/api/token"

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authHeader := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
	req.Header.Set("Authorization", "Basic "+authHeader)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errData map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errData)
		return nil, fmt.Errorf("token refresh failed: status %d, error %v", resp.StatusCode, errData)
	}

	var tokenResp SpotifyTokenResponse
	err = json.NewDecoder(resp.Body).Decode(&tokenResp)
	if err != nil {
		return nil, err
	}

	if tokenResp.RefreshToken == "" {
		tokenResp.RefreshToken = refreshToken
	}

	return &tokenResp, nil
}
