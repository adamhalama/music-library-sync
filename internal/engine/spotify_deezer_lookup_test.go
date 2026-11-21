package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/auth"
)

func TestMain(m *testing.M) {
	// Keep engine tests offline: the default lookup calls the Spotify and Deezer APIs.
	lookupDeezerTrackForSpotifyFn = func(context.Context, *spotifyDeezerLookup, string) (string, bool, error) {
		return "", false, errors.New("deezer lookup disabled in tests")
	}
	os.Exit(m.Run())
}

func newTestDeezerLookup(t *testing.T, handler http.HandlerFunc) *spotifyDeezerLookup {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	lookup := newSpotifyDeezerLookup(auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"})
	lookup.token = "test-token"
	lookup.tokenReady = true
	lookup.client = &http.Client{Transport: rewriteToServer(server.URL)}
	return lookup
}

func TestSpotifyDeezerLookupResolvesByISRC(t *testing.T) {
	lookup := newTestDeezerLookup(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "api.spotify.com" && r.URL.Path == "/v1/tracks/717Yvrk8gwaVOqNPbIATn8":
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"external_ids":{"isrc":"dech61500128"}}`))
		case r.Host == "api.deezer.com" && r.URL.Path == "/2.0/track/isrc:DECH61500128":
			_, _ = w.Write([]byte(`{"id":2106406247,"title":"Solar Fields"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	deezerID, found, err := lookup.resolve(context.Background(), "717Yvrk8gwaVOqNPbIATn8")
	if err != nil || !found || deezerID != "2106406247" {
		t.Fatalf("resolve = %q, %v, %v; want 2106406247, true, nil", deezerID, found, err)
	}
	if got := deezerTrackURL(deezerID); got != "https://www.deezer.com/track/2106406247" {
		t.Fatalf("unexpected deezer url %q", got)
	}
}

func TestSpotifyDeezerLookupReportsMissingDeezerTrack(t *testing.T) {
	lookup := newTestDeezerLookup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.spotify.com" {
			_, _ = w.Write([]byte(`{"external_ids":{"isrc":"XX0000000000"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"error":{"type":"DataException","message":"no data","code":800}}`))
	})

	deezerID, found, err := lookup.resolve(context.Background(), "717Yvrk8gwaVOqNPbIATn8")
	if err != nil || found || deezerID != "" {
		t.Fatalf("resolve = %q, %v, %v; want not found without error", deezerID, found, err)
	}
}

func TestSpotifyDeezerLookupErrorsWithoutISRC(t *testing.T) {
	lookup := newTestDeezerLookup(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"external_ids":{}}`))
	})

	if _, _, err := lookup.resolve(context.Background(), "717Yvrk8gwaVOqNPbIATn8"); err == nil || !strings.Contains(err.Error(), "no ISRC") {
		t.Fatalf("expected missing ISRC error, got %v", err)
	}
}

type rewriteToServer string

func (base rewriteToServer) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Host = req.URL.Host
	target := strings.TrimPrefix(string(base), "http://")
	clone.URL.Scheme = "http"
	clone.URL.Host = target
	return http.DefaultTransport.RoundTrip(clone)
}

func TestSpotifyDeezerLookupRefreshesExpiredToken(t *testing.T) {
	lookup := newTestDeezerLookup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.spotify.com" {
			if r.Header.Get("Authorization") != "Bearer fresh-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"external_ids":{"isrc":"DECH61500128"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":2106406247}`))
	})
	lookup.token = "expired-token"
	refreshes := 0
	lookup.fetchToken = func(context.Context, auth.SpotifyCredentials) (string, error) {
		refreshes++
		return "fresh-token", nil
	}

	deezerID, found, err := lookup.resolve(context.Background(), "717Yvrk8gwaVOqNPbIATn8")
	if err != nil || !found || deezerID != "2106406247" || refreshes != 1 {
		t.Fatalf("resolve = %q, %v, %v after %d refreshes; want one refresh and a match", deezerID, found, err, refreshes)
	}
}

func TestSpotifyDeezerLookupErrorsWithoutDeezerID(t *testing.T) {
	lookup := newTestDeezerLookup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "api.spotify.com" {
			_, _ = w.Write([]byte(`{"external_ids":{"isrc":"DECH61500128"}}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})

	if _, found, err := lookup.resolve(context.Background(), "717Yvrk8gwaVOqNPbIATn8"); err == nil || found {
		t.Fatalf("expected an error for a response without a track id, got found=%v err=%v", found, err)
	}
}
