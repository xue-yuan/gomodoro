package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func callback(t *testing.T, h http.Handler, query url.Values) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/callback?"+query.Encode(), nil))
	return rec
}

func TestOAuthCallbackRejectsWrongState(t *testing.T) {
	results := make(chan oauthCallbackResult, 1)
	h := newOAuthCallbackHandler("expected", results)

	for _, state := range []string{"", "wrong"} {
		rec := callback(t, h, url.Values{"state": {state}, "code": {"attacker-code"}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("state %q: status = %d, want 400", state, rec.Code)
		}
	}
	select {
	case res := <-results:
		t.Fatalf("request with wrong state was delivered: %+v", res)
	default:
	}
}

func TestOAuthCallbackEscapesError(t *testing.T) {
	results := make(chan oauthCallbackResult, 1)
	h := newOAuthCallbackHandler("s", results)

	rec := callback(t, h, url.Values{"state": {"s"}, "error": {"<script>alert(1)</script>"}})
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Fatalf("error parameter was not escaped: %s", rec.Body.String())
	}
	if res := <-results; res.err == nil {
		t.Fatal("expected an error result")
	}
}

func TestOAuthCallbackDeliversCodeOnce(t *testing.T) {
	results := make(chan oauthCallbackResult, 1)
	h := newOAuthCallbackHandler("s", results)

	callback(t, h, url.Values{"state": {"s"}, "code": {"first"}})
	callback(t, h, url.Values{"state": {"s"}, "code": {"second"}})

	if res := <-results; res.code != "first" {
		t.Fatalf("code = %q, want first", res.code)
	}
}

func TestSpotifyAuthURLIncludesStateAndEscapes(t *testing.T) {
	u, err := url.Parse(getSpotifyAuthURL("id&x=y", "state123"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("client_id") != "id&x=y" || q.Get("state") != "state123" || q.Get("redirect_uri") != spotifyRedirectURI {
		t.Fatalf("unexpected query: %v", q)
	}
}
