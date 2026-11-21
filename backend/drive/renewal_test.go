package drive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/lib/oauthutil"
	"github.com/rclone/rclone/lib/pacer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// A token the clock still counts as valid but Google refuses is renewed once and the request retried; a renewed token Google refuses too ends in the 401.
func TestRefusedTokenIsRenewed(t *testing.T) {
	var renewals atomic.Int32
	accepted := "renewed"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			renewals.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "renewed", "token_type": "Bearer", "expires_in": 3600})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+accepted {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":401,"message":"Request had invalid authentication credentials.","errors":[{"reason":"authError","message":"Invalid Credentials"}]}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"startPageToken": "42"})
	}))
	defer srv.Close()

	ctx := context.Background()
	expiry := time.Now().Add(30 * time.Minute).Format(time.RFC3339)
	m := configmap.Simple{"token": `{"access_token":"refused","token_type":"Bearer","refresh_token":"refresh","expiry":"` + expiry + `"}`}
	conf := &oauthutil.Config{ClientID: "id", ClientSecret: "secret", TokenURL: srv.URL + "/token"}
	client, ts, err := oauthutil.NewClientWithBaseClient(ctx, "test", m, conf, srv.Client())
	require.NoError(t, err)
	svc, err := drive.NewService(ctx, option.WithHTTPClient(client), option.WithEndpoint(srv.URL+"/"))
	require.NoError(t, err)
	f := &Fs{
		name:        "test",
		svc:         svc,
		pacer:       fs.NewPacer(ctx, pacer.NewGoogleDrive(pacer.MinSleep(time.Millisecond))),
		tokenSource: ts,
		lastRenewal: new(atomic.Int64),
	}

	token, err := f.changeNotifyStartPageToken(ctx)
	require.NoError(t, err)
	assert.Equal(t, "42", token)
	assert.Equal(t, int32(1), renewals.Load())
	assert.Contains(t, m["token"], `"access_token":"renewed"`, "the renewed token is stored")

	// Google refuses the renewed token too: no second renewal within the minute.
	accepted = "other"
	f.lastRenewal.Store(time.Now().Add(-10 * time.Second).UnixNano())
	_, err = f.changeNotifyStartPageToken(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Equal(t, int32(1), renewals.Load())
}
