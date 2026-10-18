package auth

import (
	"context"
	"errors"

	"github.com/andrinoff/linear-tui/internal/linear"
)

// ValidateAPIKey checks key by fetching the viewer.
func ValidateAPIKey(ctx context.Context, endpoint, key string) (*linear.Viewer, error) {
	svc := linear.NewService(linear.NewClient(endpoint, &linear.APIKeyTransport{Key: key}))
	v, err := svc.Viewer(ctx)
	if err != nil {
		if errors.Is(err, linear.ErrAuth) {
			return nil, errors.New("invalid API key")
		}
		return nil, errors.New(linear.Message(err))
	}
	return v, nil
}
