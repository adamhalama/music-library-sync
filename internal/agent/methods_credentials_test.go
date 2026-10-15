package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/auth"
)

func TestCredentialMethodsNeverReturnSecretValues(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentTestConfig(t, dir)
	saved := map[auth.CredentialKind]string{}
	cleared := map[auth.CredentialKind]bool{}
	ops := &CredentialOperations{
		InspectSoundCloud: func(string) auth.CredentialStatus {
			return auth.CredentialStatus{Kind: auth.CredentialKindSoundCloudClientID, Title: "SoundCloud", Health: auth.CredentialHealthAvailable, StorageSource: auth.CredentialStorageSourceKeychain}
		},
		InspectDeemix: func(string) auth.CredentialStatus {
			return auth.CredentialStatus{Kind: auth.CredentialKindDeemixARL, Title: "Deezer", Health: auth.CredentialHealthMissing}
		},
		InspectSpotify: func(string) auth.CredentialStatus {
			return auth.CredentialStatus{Kind: auth.CredentialKindSpotifyApp, Title: "Spotify", Health: auth.CredentialHealthAvailable}
		},
		SaveSoundCloud: func(value string) error { saved[auth.CredentialKindSoundCloudClientID] = value; return nil },
		SaveDeemix:     func(value string) error { saved[auth.CredentialKindDeemixARL] = value; return nil },
		SaveSpotify: func(value auth.SpotifyCredentials) error {
			saved[auth.CredentialKindSpotifyApp] = value.ClientID + ":" + value.ClientSecret
			return nil
		},
		ClearSoundCloud: func() error { cleared[auth.CredentialKindSoundCloudClientID] = true; return nil },
		ClearDeemix:     func() error { cleared[auth.CredentialKindDeemixARL] = true; return nil },
		ClearSpotify:    func() error { cleared[auth.CredentialKindSpotifyApp] = true; return nil },
		ClearFailure:    func(string, auth.CredentialKind) error { return nil },
	}
	server := &Server{WorkingDir: dir, ConfigPath: path, CredentialOps: ops}

	listed, rpcErr := server.listCredentials()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	listedJSON := string(mustJSON(t, listed))
	if strings.Contains(listedJSON, "secret") || strings.Contains(listedJSON, "value") {
		t.Fatalf("credential list exposed a value field: %s", listedJSON)
	}

	const arl = "arl-super-secret"
	savedResult, rpcErr := server.saveCredential(mustJSON(t, credentialMutationParams{Kind: auth.CredentialKindDeemixARL, Value: arl}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if saved[auth.CredentialKindDeemixARL] != arl {
		t.Fatal("credential was not passed to storage")
	}
	if strings.Contains(string(mustJSON(t, savedResult)), arl) {
		t.Fatal("save result exposed secret")
	}
	const soundCloudID = "soundcloud-secret"
	if _, rpcErr := server.saveCredential(mustJSON(t, credentialMutationParams{
		Kind: auth.CredentialKindSoundCloudClientID, Value: soundCloudID,
	})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	const spotifyID, spotifySecret = "spotify-id", "spotify-secret"
	if _, rpcErr := server.saveCredential(mustJSON(t, credentialMutationParams{
		Kind: auth.CredentialKindSpotifyApp, ClientID: spotifyID, ClientSecret: spotifySecret,
	})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if saved[auth.CredentialKindSoundCloudClientID] != soundCloudID ||
		saved[auth.CredentialKindSpotifyApp] != spotifyID+":"+spotifySecret {
		t.Fatalf("credential kinds did not reach storage: %+v", saved)
	}
	for _, kind := range []auth.CredentialKind{
		auth.CredentialKindSoundCloudClientID,
		auth.CredentialKindDeemixARL,
		auth.CredentialKindSpotifyApp,
	} {
		if _, rpcErr := server.clearCredential(mustJSON(t, credentialMutationParams{Kind: kind})); rpcErr != nil {
			t.Fatal(rpcErr)
		}
	}
	if !cleared[auth.CredentialKindSoundCloudClientID] ||
		!cleared[auth.CredentialKindDeemixARL] ||
		!cleared[auth.CredentialKindSpotifyApp] {
		t.Fatalf("not all credential kinds were cleared: %+v", cleared)
	}
}

func TestCredentialFailureRedactsKeychainErrorAndInput(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentTestConfig(t, dir)
	const secret = "do-not-leak"
	ops := defaultCredentialOperations()
	ops.SaveDeemix = func(string) error { return errors.New("User interaction is not allowed: " + secret) }
	server := &Server{WorkingDir: dir, ConfigPath: path, CredentialOps: ops}
	_, rpcErr := server.saveCredential(mustJSON(t, credentialMutationParams{Kind: auth.CredentialKindDeemixARL, Value: secret}))
	if rpcErr == nil || rpcErr.Code != CodeInternalError {
		t.Fatalf("expected credential operation error: %+v", rpcErr)
	}
	payload := string(mustJSON(t, rpcErr))
	if strings.Contains(payload, secret) {
		t.Fatalf("credential error leaked secret: %s", payload)
	}
}
