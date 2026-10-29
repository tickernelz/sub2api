package perch

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

func NewGatewayClient(creds Credentials, store CredentialStore) *Client {
	options := []SessionOption{WithClock(time.Now)}
	if store != nil {
		options = append(options, WithCredentialsPersist(func(next Credentials) {
			persistCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := store.StorePerchCredentials(persistCtx, next); err != nil {
				logger.LegacyPrintf("perch.gateway", "persist refreshed credentials failed: type=%T", err)
			}
		}))
	}
	dial := StandardDialer()
	control := NewWireDoer(dial, controlPlaneTimeout, responseHeaderTimeout)
	stream := NewWireDoer(dial, 0, responseHeaderTimeout)
	session := NewSession(creds, append(options, WithHTTPClient(control))...)
	return NewClient(session, WithClientClock(time.Now), WithClientHTTPStream(stream))
}
